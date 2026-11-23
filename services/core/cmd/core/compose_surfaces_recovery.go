package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
)

// compose_surfaces_recovery.go builds recovery (STM-05, STM-01 surfaces; REQ-VER-004, REQ-STM-001; INV-03, INV-12, INV-30, AGENT-08, AGENT-16; A330, A469, A476, A477, A486, A490, interim,
// relayed, not yet committed). Restart is the trigger (A476): one startup pass per served tenant, run in the background so that the health routes answer, and /readyz is not ready until it
// has finished. The runner it resumes with can never dispatch (recovery.NoDispatch), and its session is the stored row's (A490).

// recoverySessions is the recovery's SessionReader over the orchestrator's per-tenant stores: the tenant's own store, never another's (INV-12).
type recoverySessions struct{ rec *orchestratorRecorder }

var _ recovery.SessionReader = recoverySessions{}

// SessionOf reads the principal, role and authn reference the row holds, as the session's own tenant.
func (r recoverySessions) SessionOf(ctx context.Context, s orchestrator.Session, investigationID string) (investigations.RecordedSession, error) {
	if r.rec == nil {
		return investigations.RecordedSession{}, errors.New("cmd/core: recovery needs the orchestrator's Recorder")
	}
	st, err := r.rec.For(s.TenantID)
	if err != nil {
		return investigations.RecordedSession{}, err
	}
	return st.SessionOf(ctx, s, investigationID)
}

// recoveryWritePorts is the write ports of the recovery runner: the same Verifier, Journal and Bindings, and an Executor that always refuses. Recovery verifies what the Record shows as
// spent; spending a new grant is a person's decision, not a restart's (INV-03, INV-07, AGENT-08).
func recoveryWritePorts(wp orchestrator.WritePorts) orchestrator.WritePorts {
	wp.Executor = recovery.NoDispatch{}
	return wp
}

// recoverySettingNames is every setting of a pass, in the one order its gaps are named.
var recoverySettingNames = []string{recovery.EnvIdleMS, recovery.EnvBatchSize, recovery.EnvRunTimeoutMS}

// recoverySurface is the recovery activity and its single startup pass.
type recoverySurface struct {
	act     *recovery.Activity
	tenants []string
	bound   time.Duration
	log     *slog.Logger

	once  sync.Once
	done  chan struct{}
	ok    atomic.Bool
	fatal chan error
}

// buildRecoverySurface builds recovery. A setting that is given and invalid is an error that names it (an idle age that does not exceed the run timeout names both, A477). With none
// given recovery is the plain gap; with some absent, no reader (the Postgres list of idle investigations is not on main), no Recorder or no complete write path, the process starts and
// each reason is a gap line in fixed text, never a value.
func buildRecoverySurface(env Env, c Components, seams SurfaceSeams, rec *orchestratorRecorder, wp orchestrator.WritePorts, tenants []string, logger *slog.Logger) (*recoverySurface, []string, error) {
	var absent []string
	given := 0
	for _, name := range recoverySettingNames {
		if env.Getenv(name) == "" {
			absent = append(absent, name)
			continue
		}
		if name != recovery.EnvRunTimeoutMS { // the run timeout is shared with the workers: it alone does not ask for recovery
			given++
		}
		max := int64(1 << 31)
		if name == recovery.EnvBatchSize {
			max = 1<<31 - 1
		}
		if _, err := wholePositive(env, name, max); err != nil {
			return nil, nil, err
		}
	}
	switch {
	case given == 0:
		return nil, nil, nil
	case len(absent) > 0:
		return nil, gapLines("recovery", absent), nil
	}
	cfg, err := recovery.LoadConfig(env.Getenv)
	if err != nil {
		return nil, nil, err
	}
	var gaps []string
	if seams.Recovery == nil {
		gaps = append(gaps, "recovery: reader not built")
	}
	if rec == nil {
		gaps = append(gaps, "recovery: recorder not built")
	}
	if wp.Journal == nil {
		wp.Journal = tenantJournal{dbs: c.Records}
	}
	runner, err := buildSurfaceRunner(c, recoveryWritePorts(wp))
	if err != nil {
		return nil, nil, err
	}
	if runner == nil {
		gaps = append(gaps, "recovery: write path not built")
	}
	if len(gaps) > 0 {
		return nil, gaps, nil
	}
	act, err := recovery.New(cfg, seams.Recovery, recoverySessions{rec: rec}, wp.Journal, c.Ports.Admissions, runner)
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: recovery: %w", err)
	}
	return &recoverySurface{act: act, tenants: append([]string(nil), tenants...), bound: passBound(len(tenants), cfg), log: logger, done: make(chan struct{}), fatal: make(chan error, 1)}, nil, nil
}

// passBound is how long the startup pass may take: every tenant, at most a batch of runs each, each run bounded by the run timeout (A477). It is derived from the settings, not a number of
// its own (AGENT-16), and saturates rather than overflow.
func passBound(tenants int, cfg recovery.Config) time.Duration {
	ms := int64(tenants) * int64(cfg.Batch) * cfg.RunTimeoutMS
	if ms > math.MaxInt64/int64(time.Millisecond) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(ms) * time.Millisecond
}

// Start runs the startup pass once, in the background. A second call does nothing.
func (r *recoverySurface) Start(ctx context.Context) {
	r.once.Do(func() { go r.run(ctx) })
}

func (r *recoverySurface) run(ctx context.Context) {
	defer close(r.done)
	if len(r.tenants) == 0 {
		r.ok.Store(true)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, r.bound)
	defer cancel()
	reports, err := r.act.StartupPass(ctx, r.tenants)
	if err != nil {
		if r.log != nil {
			r.log.Error("recovery startup pass failed", "error_class", fmt.Sprintf("%T", errors.Unwrap(err)))
		}
		r.fatal <- err
		return
	}
	counts := map[recovery.Outcome]int{}
	for _, rep := range reports {
		for _, it := range rep.Items {
			counts[it.Outcome]++
		}
	}
	if r.log != nil {
		r.log.Info("recovery startup pass", "tenants", len(reports), "resumed", counts[recovery.Resumed], "needs_person", counts[recovery.NeedsPerson], "skipped", counts[recovery.Skipped],
			"failed", counts[recovery.Failed], "refused", counts[recovery.Refused], "no_recorded_session", recovery.TotalNoRecordedSession(reports))
	}
	r.ok.Store(true)
}

// Done is closed when the startup pass has ended, whether it succeeded or not.
func (r *recoverySurface) Done() <-chan struct{} { return r.done }

// Ready is true once the startup pass has completed. The reason is fixed text and names nothing else.
func (r *recoverySurface) Ready() (bool, string) {
	if r.ok.Load() {
		return true, ""
	}
	select {
	case <-r.done:
		return false, "recovery: startup pass failed"
	default:
		return false, "recovery: startup pass not finished"
	}
}

// Fatal delivers the error of a startup pass that could not read its list: the process then exits non-zero (A442).
func (r *recoverySurface) Fatal() <-chan error { return r.fatal }
