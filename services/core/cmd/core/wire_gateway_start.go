package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// wire_gateway_start.go is the production Start of the Input Gateway (EDG-01, STM-01; REQ-INP-002, REQ-INP-003, REQ-API-001; INV-12, INV-30, AGENT-10, AGENT-16; ruling A330 for the
// settings, A168 for the redelivery; interim, relayed, not yet committed). The gateway calls Start for every delivery of an admitted input, after INPUT_RECEIVED is in the Record
// (inputgateway README). Start makes the investigation of that recorded input, or finds the one an earlier Start made (investigations.Store.Start, unique per tenant and input), and
// hands the run to ONE bounded pool of workers; the gateway then answers 202 at once. The investigation is run once: a delivery that finds an investigation that already moved, or a
// run that is in flight in this process, runs nothing. Every call has a deadline and the pool is drained on shutdown.

// The settings of the investigation workers (A330: NOEMA_<AREA>_<WHAT>[_MS], no default in code). All four or none: a partial set is an error that names the missing one.
const (
	// EnvInvestigationWorkers is how many investigations run at once: the size of the one worker pool. A whole positive number.
	EnvInvestigationWorkers = "NOEMA_INVESTIGATION_WORKERS"
	// EnvInvestigationQueue is how many accepted turns may wait for a worker. A whole positive number. A full queue refuses the turn (the gateway answers 503) and the client retries.
	EnvInvestigationQueue = "NOEMA_INVESTIGATION_QUEUE"
	// EnvInvestigationRunTimeoutMS bounds one investigation's run, in whole milliseconds.
	EnvInvestigationRunTimeoutMS = "NOEMA_INVESTIGATION_RUN_TIMEOUT_MS"
	// EnvInvestigationStartTimeoutMS bounds the store call that makes or finds the investigation, in whole milliseconds.
	EnvInvestigationStartTimeoutMS = "NOEMA_INVESTIGATION_START_TIMEOUT_MS"

	// maxPoolSetting keeps a count a size a channel and a pool can be made of; it is structural, not a policy number.
	maxPoolSetting = 1 << 16
)

var (
	// ErrStartBusy marks a turn the pool could not take: the queue is full. Nothing ran and nothing is left in flight, so the client's retry is run.
	ErrStartBusy = errors.New("cmd/core: the investigation queue is full")
	// ErrStartClosed marks a turn that arrived after the shutdown began.
	ErrStartClosed = errors.New("cmd/core: the investigation workers are shut down")
	// ErrStartTurn marks a turn the Start refuses before it asks anything: no principal kind, or a correlation the core did not mint.
	ErrStartTurn = errors.New("cmd/core: the turn cannot start an investigation")

	errNoSeam = errors.New("cmd/core: the Input Gateway needs the orchestrator seam: set " + EnvInvestigationWorkers + ", " + EnvInvestigationQueue + ", " + EnvInvestigationRunTimeoutMS + " and " + EnvInvestigationStartTimeoutMS)
)

// turnRunner is the run of one investigation: orchestrator.WriteRunner (a read takes its read arrows; an allowed write walks the write path).
type turnRunner interface {
	RunWrite(ctx context.Context, s orchestrator.Session, t orchestrator.Turn) (orchestrator.Result, error)
}

// investigationStarter is the investigation store of one tenant (investigations.Store).
type investigationStarter interface {
	Start(ctx context.Context, sess orchestrator.Session, r investigations.StartRequest) (orchestrator.Investigation, bool, error)
}

// startStores gives the starter of one tenant, or an error: never another tenant's (INV-12).
type startStores interface {
	StarterFor(tenantID string) (investigationStarter, error)
}

// recorderStarts is startStores over the orchestrator's Recorder, which holds one investigation store per tenant over that tenant's own login.
type recorderStarts struct{ rec *orchestratorRecorder }

func (r recorderStarts) StarterFor(tenantID string) (investigationStarter, error) {
	st, err := r.rec.For(tenantID)
	if err != nil {
		return nil, err
	}
	return st, nil
}

// startConfig is the parsed settings of the pool.
type startConfig struct {
	Workers, Queue int
	Run, Start     time.Duration
}

func wholePositive(env Env, setting string, max int64) (int64, error) {
	raw := env.Getenv(setting)
	if !readTimeoutRe.MatchString(raw) {
		return 0, fmt.Errorf("cmd/core: %s must be a whole positive number", setting)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > max {
		return 0, fmt.Errorf("cmd/core: %s must be a whole positive number", setting) // never echoes the value
	}
	return n, nil
}

// parseStartConfig reads the four settings. None given is (zero, false, nil); some given is an error naming the first missing one; an invalid one names itself, never its value.
func parseStartConfig(env Env) (startConfig, bool, error) {
	all := []string{EnvInvestigationWorkers, EnvInvestigationQueue, EnvInvestigationRunTimeoutMS, EnvInvestigationStartTimeoutMS}
	given := 0
	for _, s := range all {
		if env.Getenv(s) != "" {
			given++
		}
	}
	if given == 0 {
		return startConfig{}, false, nil
	}
	for _, s := range all {
		if env.Getenv(s) == "" {
			return startConfig{}, false, fmt.Errorf("cmd/core: the investigation workers need %s as well: all four settings or none", s)
		}
	}
	workers, err := wholePositive(env, EnvInvestigationWorkers, maxPoolSetting)
	if err != nil {
		return startConfig{}, false, err
	}
	queue, err := wholePositive(env, EnvInvestigationQueue, maxPoolSetting)
	if err != nil {
		return startConfig{}, false, err
	}
	run, err := wholePositive(env, EnvInvestigationRunTimeoutMS, maxReadTimeoutMS)
	if err != nil {
		return startConfig{}, false, err
	}
	start, err := wholePositive(env, EnvInvestigationStartTimeoutMS, maxReadTimeoutMS)
	if err != nil {
		return startConfig{}, false, err
	}
	return startConfig{Workers: int(workers), Queue: int(queue), Run: time.Duration(run) * time.Millisecond, Start: time.Duration(start) * time.Millisecond}, true, nil
}

type startJob struct {
	key     string
	session orchestrator.Session
	turn    orchestrator.Turn
}

// turnStarter is the Start of the gateway over one bounded pool. It holds no state that outlives the process: what has run is read from the investigation's own row.
type turnStarter struct {
	cfg    startConfig
	stores startStores
	runner turnRunner
	failed func(investigationID string, err error)
	base   context.Context

	mu       sync.Mutex
	closed   bool
	queue    chan startJob
	inflight map[string]struct{}
	wg       sync.WaitGroup
}

// newTurnStarter starts the workers. failed is told of every run that ends in an error or a panic (no silent failure); it is called from a worker and must not block. The runs
// are not cancelled by ctx: a shutdown drains them, each bounded by the run timeout.
func newTurnStarter(ctx context.Context, cfg startConfig, stores startStores, runner turnRunner, failed func(string, error)) (*turnStarter, error) {
	switch {
	case stores == nil || runner == nil || failed == nil:
		return nil, errors.New("cmd/core: the investigation starter needs the stores, the runner and a failure report")
	case cfg.Workers < 1 || cfg.Queue < 1 || cfg.Run <= 0 || cfg.Start <= 0:
		return nil, errors.New("cmd/core: the investigation starter needs its four settings")
	}
	s := &turnStarter{cfg: cfg, stores: stores, runner: runner, failed: failed, base: context.WithoutCancel(ctx), queue: make(chan startJob, cfg.Queue), inflight: map[string]struct{}{}}
	for i := 0; i < cfg.Workers; i++ {
		s.wg.Add(1)
		go s.work()
	}
	return s, nil
}

func (s *turnStarter) work() {
	defer s.wg.Done()
	for j := range s.queue {
		s.run(j)
		s.mu.Lock()
		delete(s.inflight, j.key)
		s.mu.Unlock()
	}
}

func (s *turnStarter) run(j startJob) {
	defer func() {
		if recover() != nil {
			s.failed(j.turn.InvestigationID, errors.New("cmd/core: the investigation run panicked")) // the value is not kept: it can hold request data
		}
	}()
	ctx, cancel := context.WithTimeout(s.base, s.cfg.Run)
	defer cancel()
	if _, err := s.runner.RunWrite(ctx, j.session, j.turn); err != nil {
		s.failed(j.turn.InvestigationID, err)
	}
}

// Start is the gateway's seam. It returns nil when the investigation is running, has run, or is being run by this process; an error means the turn is not accepted and the
// gateway answers 503 (a retry is a redelivery, which Start handles the same way).
func (s *turnStarter) Start(ctx context.Context, t inputgateway.Turn) error {
	if t.PrincipalKind != string(session.RoleCustomer) {
		// The start route is mounted for customer sessions only (rolesCustomer). The run's role is that constant, never a value derived from the principal type: the pack's roles
		// (customer, ops, approver, audit) and the principal types (customer, operator, approver, auditor) differ, and Trust's technical authority (T6) reads the role (INV-12, A205 Q5).
		return fmt.Errorf("%w: the start route takes customer sessions only", ErrStartTurn)
	}
	invID, err := investigationIDOf(t.CorrelationID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrStartTurn, err)
	}
	if s.isClosed() {
		return ErrStartClosed
	}
	store, err := s.stores.StarterFor(t.TenantID)
	if err != nil {
		return err
	}
	sess := orchestrator.Session{TenantID: t.TenantID, PrincipalID: t.PrincipalID, Role: string(session.RoleCustomer)}
	sctx, cancel := context.WithTimeout(ctx, s.cfg.Start)
	defer cancel()
	inv, _, err := store.Start(sctx, sess, investigations.StartRequest{InvestigationID: invID, CorrelationID: t.CorrelationID, InputReceivedRef: t.Input.EventID})
	if err != nil {
		return fmt.Errorf("cmd/core: the investigation cannot be started: %w", err)
	}
	if inv.Version > 0 {
		return nil // it has moved: it was run, or is being run, and a run is never made twice
	}
	job := startJob{key: t.TenantID + "/" + t.Input.EventID, session: sess, turn: orchestrator.Turn{InvestigationID: invID, CorrelationID: t.CorrelationID, Text: t.Text, InputReceivedRef: t.Input.EventID, RequestID: t.RequestID}}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStartClosed
	}
	if _, running := s.inflight[job.key]; running {
		return nil
	}
	select {
	case s.queue <- job:
		s.inflight[job.key] = struct{}{}
		return nil
	default:
		return ErrStartBusy
	}
}

func (s *turnStarter) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Shutdown stops taking turns, lets the queued and running ones finish (each is bounded by the run timeout) and returns. Safe to call more than once.
func (s *turnStarter) Shutdown() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.queue)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// closeInReverse returns a function that runs the closers last to first, once.
func closeInReverse(closers ...func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			for i := len(closers) - 1; i >= 0; i-- {
				closers[i]()
			}
		})
	}
}

// gatewayService is the Input Gateway with its Start, its pools and its readiness. Open is true from the end of buildGatewayService until Close.
type gatewayService struct {
	Gateway *inputgateway.Gateway
	closeFn func()
	open    atomic.Bool
}

// Close stops reporting ready, then closes in reverse of opening: the gateway's per-tenant pools, then the workers (drained). Safe to call more than once.
func (g *gatewayService) Close() {
	if g == nil {
		return
	}
	g.open.Store(false)
	g.closeFn()
}

// Ready reports whether the gateway and its pools are open; the reason names the gateway.
func (g *gatewayService) Ready() (bool, string) {
	if g == nil || g.Gateway == nil {
		return false, "gateway is not configured"
	}
	if !g.open.Load() {
		return false, "gateway is not open"
	}
	return true, ""
}

// buildGatewayService builds the investigation workers, then the gateway over their Start. With none of the settings it returns no service and no error. A settings error is
// returned before any pool is opened. If the gateway cannot be built the workers are shut down. The composition's call (see status/EDG-01-wire-gateway.md):
//
//	svc, err := buildGatewayService(ctx, env, clk, ids, components.Records, recorderStarts{rec}, writeRunner, logFailed)
//
// stores is recorderStarts{rec} over buildRecorder's value, runner the *orchestrator.WriteRunner, failed a logger that records the investigation ID and the error class only.
func buildGatewayService(ctx context.Context, env Env, clk clock.Clock, ids chain.IDSource, records reconstruct.TenantDBs, stores startStores, runner turnRunner, failed func(string, error)) (*gatewayService, error) {
	cfg, configured, err := parseStartConfig(env)
	if err != nil {
		return nil, err
	}
	var starter *turnStarter
	var start func(context.Context, inputgateway.Turn) error
	if configured {
		if starter, err = newTurnStarter(ctx, cfg, stores, runner, failed); err != nil {
			return nil, err
		}
		start = starter.Start
	}
	g, closePools, err := buildGateway(ctx, env, clk, ids, records, start)
	if err != nil {
		if starter != nil {
			starter.Shutdown()
		}
		return nil, err
	}
	if g == nil {
		if starter != nil {
			starter.Shutdown()
			return nil, errors.New("cmd/core: the investigation workers are configured and the Input Gateway is not")
		}
		return nil, nil
	}
	svc := &gatewayService{Gateway: g, closeFn: closeInReverse(starter.Shutdown, closePools)}
	svc.open.Store(true)
	return svc, nil
}

// withGatewayServiceReadiness is the /readyz check of inner plus the gateway service: inner's own reason first, then the gateway until it and its pools are open.
func withGatewayServiceReadiness(inner readiness, g *gatewayService) readiness {
	return func() (bool, string) {
		if ok, reason := inner(); !ok {
			return false, reason
		}
		return g.Ready()
	}
}
