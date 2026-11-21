package main

// EDG-01 (REQ-API-001, REQ-INP-002, REQ-INP-003; INV-12, INV-07, AGENT-10, AGENT-16, INV-30): the production Start of the Input Gateway. For an admitted turn it makes (or finds) the
// investigation of the recorded input and runs it once, on ONE bounded worker pool: never twice for one recorded input, never with more workers than the setting, every call under a
// deadline, and a shutdown that drains. All identifiers and tenants are SYNTHETIC. No time.Now: the only waiting is on channels.

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

const (
	corrA  = "corr_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	invA   = "inv_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	corrB  = "corr_01ARZ3NDEKTSV4RRFFQ69G5FAW"
	reqUID = "0a0a0a0a-0a0a-4a0a-8a0a-0a0a0a0a0a0a"
)

func turnOf(tenant, corr, event string) inputgateway.Turn {
	return inputgateway.Turn{Identity: inputgateway.Identity{TenantID: tenant, PrincipalID: "usr_synth1", PrincipalKind: "customer", SessionID: "ses_" + strings.Repeat("a", 32)},
		CorrelationID: corr, RequestID: reqUID, Channel: "text", Text: "synthetic text", Input: inputgateway.Ref{EventID: event, ObjectRef: "req_01ARZ3NDEKTSV4RRFFQ69G5FAV"}}
}

type fakeStarter struct {
	mu      sync.Mutex
	created bool
	version int64
	err     error
	calls   int
	hadDL   bool
	sess    orchestrator.Session
	req     investigations.StartRequest
}

func (f *fakeStarter) Start(ctx context.Context, s orchestrator.Session, r investigations.StartRequest) (orchestrator.Investigation, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	_, f.hadDL = ctx.Deadline()
	f.sess, f.req = s, r
	if f.err != nil {
		return orchestrator.Investigation{}, false, f.err
	}
	return orchestrator.Investigation{ID: r.InvestigationID, State: "RECEIVED", Version: f.version}, f.created, nil
}

type fakeStores struct{ by map[string]*fakeStarter }

func (f fakeStores) StarterFor(tenant string) (investigationStarter, error) {
	s, ok := f.by[tenant]
	if !ok {
		return nil, ErrNoStoreForTenant
	}
	return s, nil
}

type fakeRunner struct {
	mu      sync.Mutex
	turns   []orchestrator.Turn
	sess    []orchestrator.Session
	hadDL   bool
	ctxErr  error
	running atomic.Int32
	peak    atomic.Int32
	block   chan struct{} // when non-nil each run waits for it to be closed
	started chan struct{}
	err     error
	panics  bool
}

func (r *fakeRunner) RunWrite(ctx context.Context, s orchestrator.Session, t orchestrator.Turn) (orchestrator.Result, error) {
	n := r.running.Add(1)
	defer r.running.Add(-1)
	for {
		p := r.peak.Load()
		if n <= p || r.peak.CompareAndSwap(p, n) {
			break
		}
	}
	r.mu.Lock()
	r.turns, r.sess = append(r.turns, t), append(r.sess, s)
	_, r.hadDL = ctx.Deadline()
	r.ctxErr = ctx.Err()
	r.mu.Unlock()
	if r.started != nil {
		r.started <- struct{}{}
	}
	if r.block != nil {
		<-r.block
	}
	if r.panics {
		panic("synthetic panic")
	}
	return orchestrator.Result{}, r.err
}

func (r *fakeRunner) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.turns) }

type failures struct {
	mu   sync.Mutex
	invs []string
	errs []error
}

func (f *failures) report(inv string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invs, f.errs = append(f.invs, inv), append(f.errs, err)
}

func cfg(workers, queue int) startConfig {
	return startConfig{Workers: workers, Queue: queue, Run: 1 << 40, Start: 1 << 40}
}

func newStarter(t *testing.T, c startConfig, st *fakeStarter, r *fakeRunner, f *failures) *turnStarter {
	t.Helper()
	s, err := newTurnStarter(t.Context(), c, fakeStores{by: map[string]*fakeStarter{tenA: st}}, r, f.report)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	return s
}

// REQ-INP-002, INV-12: the run is made for the turn's own session and the recorded input, with the investigation ID of the correlation (A234).
func TestAnAdmittedTurnRunsOnceAsTheIdentitysSession(t *testing.T) {
	st, r, f := &fakeStarter{created: true}, &fakeRunner{}, &failures{}
	s := newStarter(t, cfg(2, 4), st, r, f)
	tn := turnOf(tenA, corrA, "recev_AAA1")
	if err := s.Start(t.Context(), tn); err != nil {
		t.Fatal(err)
	}
	s.Shutdown()
	if r.count() != 1 {
		t.Fatalf("runs = %d, want 1", r.count())
	}
	want := orchestrator.Turn{InvestigationID: invA, CorrelationID: corrA, Text: "synthetic text", InputReceivedRef: "recev_AAA1", RequestID: reqUID}
	if r.turns[0] != want {
		t.Fatalf("turn = %+v, want %+v", r.turns[0], want)
	}
	if r.sess[0] != (orchestrator.Session{TenantID: tenA, PrincipalID: "usr_synth1", Role: "customer"}) {
		t.Fatalf("session = %+v", r.sess[0])
	}
	if st.req.InvestigationID != invA || st.req.CorrelationID != corrA || st.req.InputReceivedRef != "recev_AAA1" {
		t.Fatalf("start request = %+v", st.req)
	}
	if len(f.errs) != 0 {
		t.Fatalf("unexpected failure %v", f.errs)
	}
}

// REQ-INP-003, A168: a replay of an input whose investigation already moved never runs it again; one that never moved (a crash before the first move) is run.
func TestAReplayOfAnInputThatAlreadyMovedDoesNotRunTwice(t *testing.T) {
	st, r, f := &fakeStarter{created: false, version: 3}, &fakeRunner{}, &failures{}
	s := newStarter(t, cfg(1, 2), st, r, f)
	if err := s.Start(t.Context(), turnOf(tenA, corrA, "recev_AAA1")); err != nil {
		t.Fatal(err)
	}
	s.Shutdown()
	if r.count() != 0 {
		t.Fatalf("runs = %d, want 0 for an investigation at version 3", r.count())
	}
}

func TestAnInvestigationThatNeverMovedIsRunOnARedelivery(t *testing.T) {
	st, r, f := &fakeStarter{created: false, version: 0}, &fakeRunner{}, &failures{}
	s := newStarter(t, cfg(1, 2), st, r, f)
	if err := s.Start(t.Context(), turnOf(tenA, corrA, "recev_AAA1")); err != nil {
		t.Fatal(err)
	}
	s.Shutdown()
	if r.count() != 1 {
		t.Fatalf("runs = %d, want 1", r.count())
	}
}

// INV-07 spirit: a redelivery while the first run is still in flight is not a second run.
func TestARedeliveryWhileTheRunIsInFlightDoesNotRunTwice(t *testing.T) {
	st, r, f := &fakeStarter{created: true}, &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 4)}, &failures{}
	s := newStarter(t, cfg(2, 4), st, r, f)
	if err := s.Start(t.Context(), turnOf(tenA, corrA, "recev_AAA1")); err != nil {
		t.Fatal(err)
	}
	<-r.started
	st.mu.Lock()
	st.created = false // the store now finds the row, still at version 0 until the first move lands
	st.mu.Unlock()
	if err := s.Start(t.Context(), turnOf(tenA, corrA, "recev_AAA1")); err != nil {
		t.Fatal(err)
	}
	close(r.block)
	s.Shutdown()
	if r.count() != 1 {
		t.Fatalf("runs = %d, want 1", r.count())
	}
}

// AGENT-16: never more runs at once than the setting.
func TestNeverMoreRunsAtOnceThanTheWorkerSetting(t *testing.T) {
	st, r, f := &fakeStarter{created: true}, &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 16)}, &failures{}
	s := newStarter(t, cfg(2, 8), st, r, f)
	corrs := []string{"corr_01ARZ3NDEKTSV4RRFFQ69G5FA1", "corr_01ARZ3NDEKTSV4RRFFQ69G5FA2", "corr_01ARZ3NDEKTSV4RRFFQ69G5FA3", "corr_01ARZ3NDEKTSV4RRFFQ69G5FA4", "corr_01ARZ3NDEKTSV4RRFFQ69G5FA5"}
	for i, c := range corrs {
		if err := s.Start(t.Context(), turnOf(tenA, c, "recev_E"+string(rune('A'+i))+"AA")); err != nil {
			t.Fatal(err)
		}
	}
	<-r.started
	<-r.started
	close(r.block)
	s.Shutdown()
	if r.count() != 5 {
		t.Fatalf("runs = %d, want 5", r.count())
	}
	if r.peak.Load() != 2 {
		t.Fatalf("peak concurrency = %d, want 2", r.peak.Load())
	}
}

// A full queue is an error the gateway answers with 503, and it leaves nothing in flight: the client's retry is run.
func TestAFullQueueRefusesAndTheRetryIsRun(t *testing.T) {
	st, r, f := &fakeStarter{created: true}, &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 4)}, &failures{}
	s := newStarter(t, cfg(1, 1), st, r, f)
	if err := s.Start(t.Context(), turnOf(tenA, "corr_01ARZ3NDEKTSV4RRFFQ69G5FA1", "recev_AA1")); err != nil {
		t.Fatal(err)
	}
	<-r.started // the worker holds the first
	if err := s.Start(t.Context(), turnOf(tenA, "corr_01ARZ3NDEKTSV4RRFFQ69G5FA2", "recev_AA2")); err != nil {
		t.Fatal(err) // fills the queue
	}
	err := s.Start(t.Context(), turnOf(tenA, corrB, "recev_AA3"))
	if !errors.Is(err, ErrStartBusy) {
		t.Fatalf("error = %v, want ErrStartBusy", err)
	}
	close(r.block)
	s.Shutdown()
	if r.count() != 2 {
		t.Fatalf("runs = %d, want 2 (the refused turn did not run)", r.count())
	}
}

func TestARefusedTurnIsRunWhenItIsRedeliveredLater(t *testing.T) {
	st, r, f := &fakeStarter{created: true}, &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 4)}, &failures{}
	s := newStarter(t, cfg(1, 1), st, r, f)
	_ = s.Start(t.Context(), turnOf(tenA, "corr_01ARZ3NDEKTSV4RRFFQ69G5FA1", "recev_AA1"))
	<-r.started
	_ = s.Start(t.Context(), turnOf(tenA, "corr_01ARZ3NDEKTSV4RRFFQ69G5FA2", "recev_AA2"))
	if err := s.Start(t.Context(), turnOf(tenA, corrB, "recev_AA3")); !errors.Is(err, ErrStartBusy) {
		t.Fatalf("error = %v, want ErrStartBusy", err)
	}
	close(r.block)
	// wait until the queue has room again by draining through a second Start loop is not possible without time; Shutdown drains, so use a fresh starter on the same fakes.
	s.Shutdown()
	s2 := newStarter(t, cfg(1, 1), st, r, f)
	st.mu.Lock()
	st.created = false
	st.mu.Unlock()
	if err := s2.Start(t.Context(), turnOf(tenA, corrB, "recev_AA3")); err != nil {
		t.Fatal(err)
	}
	s2.Shutdown()
	if r.count() != 3 {
		t.Fatalf("runs = %d, want 3", r.count())
	}
}

// AGENT-16, no unbounded call: the store call and the run each carry a deadline.
func TestEveryCallCarriesADeadline(t *testing.T) {
	st, r, f := &fakeStarter{created: true}, &fakeRunner{}, &failures{}
	s := newStarter(t, cfg(1, 1), st, r, f)
	if err := s.Start(context.Background(), turnOf(tenA, corrA, "recev_AAA1")); err != nil {
		t.Fatal(err)
	}
	s.Shutdown()
	if !st.hadDL || !r.hadDL {
		t.Fatalf("deadline on the store call: %v, on the run: %v; both must be true", st.hadDL, r.hadDL)
	}
}

// EC-E16 / no silent failure: a store that cannot start the investigation fails the Start (the gateway answers 503) and nothing runs; a tenant with no store likewise.
func TestAStoreErrorFailsTheStartAndNothingRuns(t *testing.T) {
	boom := errors.New("synthetic store failure")
	st, r, f := &fakeStarter{err: boom}, &fakeRunner{}, &failures{}
	s := newStarter(t, cfg(1, 1), st, r, f)
	if err := s.Start(t.Context(), turnOf(tenA, corrA, "recev_AAA1")); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the store's", err)
	}
	if err := s.Start(t.Context(), turnOf(tenB, corrA, "recev_AAA1")); !errors.Is(err, ErrNoStoreForTenant) {
		t.Fatalf("error = %v, want ErrNoStoreForTenant", err)
	}
	s.Shutdown()
	if r.count() != 0 {
		t.Fatalf("runs = %d, want 0", r.count())
	}
}

// INV-12: a turn with no principal kind, or a correlation that is not the core's, never starts anything.
func TestATurnWithoutAKindOrACoreCorrelationIsRefused(t *testing.T) {
	st, r, f := &fakeStarter{created: true}, &fakeRunner{}, &failures{}
	s := newStarter(t, cfg(1, 1), st, r, f)
	noKind := turnOf(tenA, corrA, "recev_AAA1")
	noKind.PrincipalKind = ""
	if err := s.Start(t.Context(), noKind); err == nil {
		t.Fatal("a turn with no principal kind started")
	}
	if err := s.Start(t.Context(), turnOf(tenA, "corr_NONE", "recev_AAA1")); err == nil {
		t.Fatal("a turn with a correlation that is not the core's started")
	}
	s.Shutdown()
	if st.calls != 0 || r.count() != 0 {
		t.Fatalf("store calls %d, runs %d; want none", st.calls, r.count())
	}
}

// No silent failure: a run that fails, and a run that panics, are reported with the investigation ID and never crash the worker.
func TestAFailedRunAndAPanicAreReported(t *testing.T) {
	boom := errors.New("synthetic run failure")
	st, r, f := &fakeStarter{created: true}, &fakeRunner{err: boom}, &failures{}
	s := newStarter(t, cfg(1, 2), st, r, f)
	if err := s.Start(t.Context(), turnOf(tenA, corrA, "recev_AAA1")); err != nil {
		t.Fatal(err)
	}
	s.Shutdown()
	if len(f.errs) != 1 || f.invs[0] != invA || !errors.Is(f.errs[0], boom) {
		t.Fatalf("reported %v %v", f.invs, f.errs)
	}
	r2, f2 := &fakeRunner{panics: true}, &failures{}
	s2 := newStarter(t, cfg(1, 2), &fakeStarter{created: true}, r2, f2)
	if err := s2.Start(t.Context(), turnOf(tenA, corrA, "recev_AAA1")); err != nil {
		t.Fatal(err)
	}
	s2.Shutdown()
	if len(f2.errs) != 1 {
		t.Fatalf("a panic was not reported: %v", f2.errs)
	}
}

// Shutdown drains what was accepted, refuses what comes after, and is safe to repeat.
func TestShutdownDrainsAcceptedRunsAndRefusesNewOnes(t *testing.T) {
	st, r, f := &fakeStarter{created: true}, &fakeRunner{block: make(chan struct{}), started: make(chan struct{}, 4)}, &failures{}
	s := newStarter(t, cfg(1, 1), st, r, f)
	_ = s.Start(t.Context(), turnOf(tenA, "corr_01ARZ3NDEKTSV4RRFFQ69G5FA1", "recev_AA1"))
	<-r.started
	_ = s.Start(t.Context(), turnOf(tenA, "corr_01ARZ3NDEKTSV4RRFFQ69G5FA2", "recev_AA2"))
	done := make(chan struct{})
	go func() { s.Shutdown(); close(done) }()
	for !errors.Is(s.Start(t.Context(), turnOf(tenA, corrB, "recev_AA3")), ErrStartClosed) { // busy until the shutdown has begun
		runtime.Gosched()
	}
	close(r.block)
	<-done
	if r.count() != 2 {
		t.Fatalf("runs = %d, want both accepted runs drained", r.count())
	}
	if err := s.Start(t.Context(), turnOf(tenA, corrB, "recev_AA4")); !errors.Is(err, ErrStartClosed) {
		t.Fatalf("error = %v, want ErrStartClosed", err)
	}
	s.Shutdown()
}

// A330 / AGENT-16: no defaults; a partial configuration names the missing setting; an invalid one names the setting and never the value.
func startEnv() envMap {
	return envMap{EnvInvestigationWorkers: "2", EnvInvestigationQueue: "8", EnvInvestigationRunTimeoutMS: "30000", EnvInvestigationStartTimeoutMS: "2000"}
}

func TestStartSettingsNoneIsNotConfiguredAndNoError(t *testing.T) {
	_, ok, err := parseStartConfig(osEnv(envMap{}))
	if err != nil || ok {
		t.Fatalf("configured %v, error %v; want neither", ok, err)
	}
}

func TestStartSettingsFullParse(t *testing.T) {
	c, ok, err := parseStartConfig(osEnv(startEnv()))
	if err != nil || !ok || c.Workers != 2 || c.Queue != 8 || c.Run.Milliseconds() != 30000 || c.Start.Milliseconds() != 2000 {
		t.Fatalf("got %+v %v %v", c, ok, err)
	}
}

func TestAPartialStartConfigurationNamesTheMissingSetting(t *testing.T) {
	for _, miss := range []string{EnvInvestigationWorkers, EnvInvestigationQueue, EnvInvestigationRunTimeoutMS, EnvInvestigationStartTimeoutMS} {
		e := startEnv()
		delete(e, miss)
		_, _, err := parseStartConfig(osEnv(e))
		if err == nil || !strings.Contains(err.Error(), miss) {
			t.Fatalf("missing %s: error = %v, want one naming it", miss, err)
		}
	}
}

func TestAnInvalidStartSettingNamesItAndNeverTheValue(t *testing.T) {
	for _, s := range []string{EnvInvestigationWorkers, EnvInvestigationQueue, EnvInvestigationRunTimeoutMS, EnvInvestigationStartTimeoutMS} {
		for _, bad := range []string{"0", "-1", "01", "1.5", "abc", " 2", "99999999999999999999"} {
			e := startEnv()
			e[s] = bad
			_, _, err := parseStartConfig(osEnv(e))
			if err == nil || !strings.Contains(err.Error(), s) || strings.Contains(err.Error(), bad) {
				t.Fatalf("%s=%q: error = %v, want one naming the setting and not the value", s, bad, err)
			}
		}
	}
}

func TestANilPortRefusesTheStarter(t *testing.T) {
	if _, err := newTurnStarter(t.Context(), cfg(1, 1), nil, &fakeRunner{}, (&failures{}).report); err == nil {
		t.Fatal("no stores accepted")
	}
	if _, err := newTurnStarter(t.Context(), cfg(1, 1), fakeStores{}, nil, (&failures{}).report); err == nil {
		t.Fatal("no runner accepted")
	}
	if _, err := newTurnStarter(t.Context(), cfg(1, 1), fakeStores{}, &fakeRunner{}, nil); err == nil {
		t.Fatal("no failure report accepted")
	}
	if _, err := newTurnStarter(t.Context(), startConfig{}, fakeStores{}, &fakeRunner{}, (&failures{}).report); err == nil {
		t.Fatal("a zero configuration accepted")
	}
}

// Close order: reverse of opening, once.
func TestCloseRunsInReverseOrderAndOnlyOnce(t *testing.T) {
	var order []int
	c := closeInReverse(func() { order = append(order, 1) }, func() { order = append(order, 2) }, func() { order = append(order, 3) })
	c()
	c()
	if len(order) != 3 || order[0] != 3 || order[1] != 2 || order[2] != 1 {
		t.Fatalf("order = %v, want [3 2 1] once", order)
	}
}

// The service: readiness names the gateway until it and its pools are open, and is false again after Close.
func TestTheServiceIsReadyOnlyWhileOpenAndNamesTheGatewayOtherwise(t *testing.T) {
	d := newGwDirs(t, tenA)
	e := d.env()
	for k, v := range startEnv() {
		e[k] = v
	}
	records := recordsOf(t, e)
	clkEnv := osEnv(e)
	svc, err := buildGatewayServiceForTest(t, clkEnv, records)
	if err != nil || svc == nil || svc.Gateway == nil {
		t.Fatalf("service %v, error %v", svc, err)
	}
	inner := func() (bool, string) { return true, "" }
	check := withGatewayServiceReadiness(inner, svc)
	if ok, reason := check(); !ok {
		t.Fatalf("not ready while open: %q", reason)
	}
	svc.Close()
	svc.Close()
	ok, reason := check()
	if ok || !strings.Contains(reason, "gateway") {
		t.Fatalf("ready %v, reason %q after Close; want not ready and the gateway named", ok, reason)
	}
	var nilSvc *gatewayService
	if ok, reason := withGatewayServiceReadiness(inner, nilSvc)(); ok || !strings.Contains(reason, "gateway") {
		t.Fatalf("no service: ready %v, reason %q", ok, reason)
	}
	if ok, reason := withGatewayServiceReadiness(func() (bool, string) { return false, "db down" }, svc)(); ok || reason != "db down" {
		t.Fatalf("inner reason first: %v %q", ok, reason)
	}
}

func TestTheServiceNamesTheStartSettingsWhenOnlyTheGatewayIsConfigured(t *testing.T) {
	d := newGwDirs(t, tenA)
	e := d.env()
	_, err := buildGatewayServiceForTest(t, osEnv(e), recordsOf(t, e))
	if err == nil || !strings.Contains(err.Error(), EnvInvestigationWorkers) {
		t.Fatalf("error = %v, want one naming %s", err, EnvInvestigationWorkers)
	}
}

func TestTheServiceWithNoSettingsAtAllIsNoServiceAndNoError(t *testing.T) {
	svc, err := buildGatewayServiceForTest(t, osEnv(envMap{}), nil)
	if err != nil || svc != nil {
		t.Fatalf("service %v, error %v; want neither", svc, err)
	}
}

func TestAnInvalidStartSettingStopsTheServiceBeforeAnyPoolIsOpened(t *testing.T) {
	d := newGwDirs(t, tenA)
	e := d.env()
	for k, v := range startEnv() {
		e[k] = v
	}
	e[EnvInvestigationWorkers] = "0"
	svc, err := buildGatewayServiceForTest(t, osEnv(e), recordsOf(t, e))
	if svc != nil || err == nil || !strings.Contains(err.Error(), EnvInvestigationWorkers) {
		t.Fatalf("service %v, error %v; want none and one naming the setting", svc, err)
	}
}

func buildGatewayServiceForTest(t *testing.T, env Env, records reconstruct.TenantDBs) (*gatewayService, error) {
	t.Helper()
	clk := clock.NewManual(apiEpoch)
	return buildGatewayService(t.Context(), env, clk, ids.New(clk, nil), records, fakeStores{by: map[string]*fakeStarter{tenA: {}}}, &fakeRunner{}, (&failures{}).report)
}

// A run that finished frees its input: a redelivery of an investigation that never moved is run again, not remembered as in flight for ever.
func TestAFinishedRunFreesItsInputForARedelivery(t *testing.T) {
	st, r, f := &fakeStarter{created: false, version: 0}, &fakeRunner{}, &failures{}
	s := newStarter(t, cfg(1, 1), st, r, f)
	for i := 0; r.count() < 2; i++ {
		if i > 1_000_000 {
			t.Fatal("a finished run still holds its input")
		}
		_ = s.Start(t.Context(), turnOf(tenA, corrA, "recev_AAA1"))
		runtime.Gosched()
	}
}

// After the shutdown began nothing is asked of the store.
func TestAfterShutdownTheStoreIsNotAsked(t *testing.T) {
	st, r, f := &fakeStarter{created: true}, &fakeRunner{}, &failures{}
	s := newStarter(t, cfg(1, 1), st, r, f)
	s.Shutdown()
	if err := s.Start(t.Context(), turnOf(tenA, corrA, "recev_AAA1")); !errors.Is(err, ErrStartClosed) || st.calls != 0 {
		t.Fatalf("error %v, store calls %d", err, st.calls)
	}
}

// A drain is not cut short by the process context: a run is bounded by its own timeout, not cancelled with the parent.
func TestARunIsNotCancelledWithTheStartersContext(t *testing.T) {
	st, r, f := &fakeStarter{created: true}, &fakeRunner{}, &failures{}
	ctx, cancel := context.WithCancel(t.Context())
	s, err := newTurnStarter(ctx, cfg(1, 1), fakeStores{by: map[string]*fakeStarter{tenA: st}}, r, f.report)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := s.Start(context.Background(), turnOf(tenA, corrA, "recev_AAA1")); err != nil {
		t.Fatal(err)
	}
	s.Shutdown()
	if r.count() != 1 || r.ctxErr != nil {
		t.Fatalf("runs %d, run context error %v; want 1 and none", r.count(), r.ctxErr)
	}
}

func TestThePoolSizeHasAStructuralCeiling(t *testing.T) {
	e := startEnv()
	e[EnvInvestigationWorkers] = "65536"
	if _, ok, err := parseStartConfig(osEnv(e)); err != nil || !ok {
		t.Fatalf("65536 refused: %v", err)
	}
	e[EnvInvestigationWorkers] = "65537"
	if _, _, err := parseStartConfig(osEnv(e)); err == nil {
		t.Fatal("65537 accepted")
	}
}

// INV-12, A205 Q5, A255: the start route is mounted for customer sessions only, so the run's role is the constant "customer", never a value derived from the principal type (the
// pack's roles are customer, ops, approver and audit; the principal types are customer, operator, approver and auditor, which differ). A turn of any other kind is refused before
// anything is asked: nothing is read, started or run.
func TestATurnOfAPrincipalThatIsNotACustomerIsRefusedBeforeAnythingIsAsked(t *testing.T) {
	for _, kind := range []string{"operator", "ops", "approver", "auditor", "audit", "Customer", "customer "} {
		st, r, f := &fakeStarter{created: true}, &fakeRunner{}, &failures{}
		s := newStarter(t, cfg(2, 4), st, r, f)
		tn := turnOf(tenA, corrA, "recev_AAA1")
		tn.PrincipalKind = kind
		if err := s.Start(t.Context(), tn); !errors.Is(err, ErrStartTurn) {
			t.Errorf("kind %q: err = %v, want ErrStartTurn", kind, err)
		}
		s.Shutdown()
		if st.calls != 0 || r.count() != 0 {
			t.Errorf("kind %q: the store was asked %d times and %d runs happened", kind, st.calls, r.count())
		}
	}
}
