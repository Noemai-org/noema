package main

// STM-04 wire lane (card status/STM-04.md, lane note status/STM-04-wire-worker.md): buildWorker plugs the real ports of the workflows' activities into the worker and answers readiness.
// REQ-VER-004, REQ-EXE-006; INV-30, INV-07, AGENT-08, AGENT-16. The Temporal connection is the seam of worker_run.go, so no server is needed. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/workflows"
)

type seenCtx struct {
	deadline time.Time
	has      bool
}

type probeExecutor struct{ seen []seenCtx }

func (p *probeExecutor) ExecuteGrant(ctx context.Context, _, g string) (workflows.GrantRun, error) {
	d, ok := ctx.Deadline()
	p.seen = append(p.seen, seenCtx{d, ok})
	return workflows.GrantRun{GrantID: g}, errors.New("synthetic failure")
}

type probeReader struct{ seen []seenCtx }

func (p *probeReader) ReadCaseByKey(ctx context.Context, _, _ string) (workflows.CaseView, error) {
	d, ok := ctx.Deadline()
	p.seen = append(p.seen, seenCtx{d, ok})
	return workflows.CaseView{}, nil
}

type probeIssuer struct{ seen []seenCtx }

func (p *probeIssuer) IssueRouteGrant(ctx context.Context, _ workflows.RouteGrantRequest) (string, error) {
	d, ok := ctx.Deadline()
	p.seen = append(p.seen, seenCtx{d, ok})
	return "grt_x", nil
}

// capWorker keeps the activities value that was registered.
type capWorker struct {
	acts  any
	log   *[]string
	start error
}

func (w *capWorker) RegisterWorkflowWithOptions(any, workflow.RegisterOptions) {}
func (w *capWorker) RegisterActivity(a any)                                    { w.acts = a }
func (w *capWorker) Start() error                                              { *w.log = append(*w.log, "start"); return w.start }
func (w *capWorker) Stop()                                                     { *w.log = append(*w.log, "stop") }

type capConn struct {
	log *[]string
	w   *capWorker
}

func (c *capConn) NewWorker(string) temporalWorker { return c.w }
func (c *capConn) Close()                          { *c.log = append(*c.log, "close") }

func newCap(startErr error) (*capConn, *[]string) {
	log := &[]string{}
	return &capConn{log: log, w: &capWorker{log: log, start: startErr}}, log
}

func dialOf(c Connection, calls *int) func(context.Context, *WorkerConfig) (Connection, error) {
	return func(context.Context, *WorkerConfig) (Connection, error) { *calls++; return c, nil }
}

func goodPorts() (workerPorts, *probeExecutor, *probeReader, *probeIssuer) {
	e, r, i := &probeExecutor{}, &probeReader{}, &probeIssuer{}
	return workerPorts{Executor: e, Reader: r, Issuer: i}, e, r, i
}

// REQ-OPS-003: with no Temporal setting there is no worker and no error, and readiness says the worker is not running.
func TestNoTemporalSettingsBuildNoWorkerAndReadinessNamesIt(t *testing.T) {
	calls := 0
	ports, _, _, _ := goodPorts()
	h, err := buildWorker(deadlineCtx(), workerEnv(nil), ports, func(context.Context, *WorkerConfig) (Connection, error) { calls++; return nil, errors.New("x") })
	if err != nil || h != nil || calls != 0 {
		t.Fatalf("handle %v, err %v, dials %d; want none", h, err, calls)
	}
	ok, why := h.Readiness()()
	if ok || !strings.Contains(why, "worker") {
		t.Fatalf("ready %v, reason %q; want not ready, naming worker", ok, why)
	}
}

// REQ-EXE-006: a started worker is ready; once stopped it is not, and it names the worker; the connection closes once.
func TestAStartedWorkerIsReadyAndAStoppedOneIsNot(t *testing.T) {
	conn, log := newCap(nil)
	calls := 0
	ports, _, _, _ := goodPorts()
	h, err := buildWorker(deadlineCtx(), workerEnv(drainWorker()), ports, dialOf(conn, &calls))
	if err != nil || h == nil || calls != 1 {
		t.Fatalf("handle %v, err %v, dials %d", h, err, calls)
	}
	if ok, why := h.Readiness()(); !ok || why != "" {
		t.Fatalf("started worker: ready %v, reason %q", ok, why)
	}
	h.Stop()
	h.Stop()
	ok, why := h.Readiness()()
	if ok || !strings.Contains(why, "worker") {
		t.Fatalf("stopped worker: ready %v, reason %q", ok, why)
	}
	if got := strings.Join(*log, ","); got != "start,stop,close" {
		t.Fatalf("events %q, want start,stop,close once", got)
	}
}

// AGENT-16, INV-30: a worker that cannot start leaves no handle, closes what it opened and reports ErrWorkerNotStarted without an address.
func TestAWorkerThatDoesNotStartIsNotReady(t *testing.T) {
	conn, log := newCap(errors.New("boom secret-host:7233"))
	calls := 0
	ports, _, _, _ := goodPorts()
	h, err := buildWorker(deadlineCtx(), workerEnv(drainWorker()), ports, dialOf(conn, &calls))
	if h != nil || !errors.Is(err, ErrWorkerNotStarted) || strings.Contains(err.Error(), "secret-host") {
		t.Fatalf("handle %v, err %v", h, err)
	}
	if got := strings.Join(*log, ","); got != "start,close" {
		t.Fatalf("events %q, want start,close", got)
	}
	if ok, why := h.Readiness()(); ok || !strings.Contains(why, "worker") {
		t.Fatalf("ready %v, reason %q", ok, why)
	}
}

// AGENT-16: a bad setting, or a missing port, is an error that names it, and nothing is dialled. A half worker is never built.
func TestAMissingPortOrBadSettingIsRefusedBeforeADial(t *testing.T) {
	bad := drainWorker()
	bad["NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS"] = "0"
	cases := map[string]struct {
		env   map[string]string
		mut   func(*workerPorts)
		names string
	}{
		"no executor": {drainWorker(), func(p *workerPorts) { p.Executor = nil }, "executor"},
		"no reader":   {drainWorker(), func(p *workerPorts) { p.Reader = nil }, "reader"},
		"no issuer":   {drainWorker(), func(p *workerPorts) { p.Issuer = nil }, "issuer"},
		"no timeout":  {bad, func(*workerPorts) {}, "NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS"},
	}
	for name, c := range cases {
		conn, _ := newCap(nil)
		calls := 0
		ports, _, _, _ := goodPorts()
		c.mut(&ports)
		h, err := buildWorker(deadlineCtx(), workerEnv(c.env), ports, dialOf(conn, &calls))
		if h != nil || err == nil || !strings.Contains(err.Error(), c.names) || calls != 0 {
			t.Errorf("%s: handle %v, err %v, dials %d", name, h, err, calls)
		}
	}
}

// REQ-EXE-005, INV-30: every activity call is bounded by the configured activity timeout (NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS) and runs once per call, with the ports' errors passed on.
func TestEveryActivityPortIsBoundedByTheConfiguredTimeout(t *testing.T) {
	conn, _ := newCap(nil)
	calls := 0
	ports, e, r, i := goodPorts()
	if _, err := buildWorker(deadlineCtx(), workerEnv(drainWorker()), ports, dialOf(conn, &calls)); err != nil {
		t.Fatal(err)
	}
	acts, ok := conn.w.acts.(*workflows.EscalateActivities)
	if !ok {
		t.Fatalf("registered %T, want *workflows.EscalateActivities", conn.w.acts)
	}
	start := clock.System{}.Now()
	bg := context.Background() // no deadline of its own: the bound must come from the port
	if _, err := acts.ExecuteGrant(bg, "ten_a", "grt_1"); err == nil {
		t.Fatal("the executor's error was swallowed")
	}
	_, _ = acts.ReadBackCase(bg, "ten_a", "idk")
	_, _ = acts.IssueRouteGrant(bg, workflows.RouteGrantRequest{TenantID: "ten_a"})
	for name, seen := range map[string][]seenCtx{"executor": e.seen, "reader": r.seen, "issuer": i.seen} {
		if len(seen) != 1 || !seen[0].has {
			t.Fatalf("%s: calls %+v, want exactly one with a deadline", name, seen)
		}
		if left := seen[0].deadline.Sub(start); left <= 0 || left > 31*time.Second {
			t.Fatalf("%s: deadline in %v, want within the configured 30s", name, left)
		}
	}
}

// INV-30: the activities the worker registers have the ports of the workflows' own table and nothing else.
func TestTheRegisteredActivitiesAreTheWorkflowsOwnType(t *testing.T) {
	conn, _ := newCap(nil)
	calls := 0
	ports, _, _, _ := goodPorts()
	if _, err := buildWorker(deadlineCtx(), workerEnv(drainWorker()), ports, dialOf(conn, &calls)); err != nil {
		t.Fatal(err)
	}
	if got := reflect.TypeOf(conn.w.acts); got != reflect.TypeOf(&workflows.EscalateActivities{}) {
		t.Fatalf("registered %v", got)
	}
}
