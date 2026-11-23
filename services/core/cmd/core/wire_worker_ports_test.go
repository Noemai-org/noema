package main

// STM-04 wire lane, part 2: the real activity ports, the Trust-only route issuer, readiness and graceful shutdown of the worker.
// REQ-EXE-006, REQ-EXE-005, REQ-VER-004; INV-03, INV-30, INV-07, AGENT-08, AGENT-16. Every value is SYNTHETIC.

import (
	"context"
	"encoding/hex"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/workflows"
)

func drainWorker() map[string]string {
	m := goodWorker()
	m["NOEMA_TEMPORAL_SHUTDOWN_DRAIN_MS"] = "200"
	return m
}

// ---- the route issuer ----

type deciderFake struct {
	got    []RouteDecision
	hasDL  bool
	result RouteDecisionResult
	err    error
}

func (d *deciderFake) DecideRoute(ctx context.Context, r RouteDecision) (RouteDecisionResult, error) {
	d.got = append(d.got, r)
	_, d.hasDL = ctx.Deadline()
	return d.result, d.err
}

func routeReq() workflows.RouteGrantRequest {
	return workflows.RouteGrantRequest{TenantID: "ten_a", CorrelationID: "cor_1", CaseID: "case_1", VersionToken: "v7", PlannedQueue: "disputes_l2", CreateReceiptID: "rcp_1"}
}

func allowOne() RouteDecisionResult {
	return RouteDecisionResult{Outcome: "ALLOW", DecisionID: "dec_1", GrantIDs: []string{"grt_route1"}}
}

// INV-03, REQ-EXE-006: the route grant is the id Trust's decision returned, asked for as its own decision on the route capability, bound to the case and the owner queue.
func TestRouteIssuerAsksTrustForTheRouteCapabilityAndReturnsTrustsGrant(t *testing.T) {
	d := &deciderFake{result: allowOne()}
	iss, err := newRouteGrantIssuer(d)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	id, err := iss.IssueRouteGrant(ctx, routeReq())
	if err != nil || id != "grt_route1" {
		t.Fatalf("id %q, err %v", id, err)
	}
	want := RouteDecision{TenantID: "ten_a", CorrelationID: "cor_1", CaseID: "case_1", OwnerQueue: "disputes_l2", CapabilityID: bridge.CapRouteToInternalOwner}
	if len(d.got) != 1 || d.got[0] != want || !d.hasDL {
		t.Fatalf("decider saw %+v (deadline %v), want one call %+v", d.got, d.hasDL, want)
	}
}

// INV-03, INV-01: anything but one ALLOW with a decision and exactly one well-formed grant is no grant; the workflow stops.
func TestRouteIssuerIssuesNothingUnlessTrustAllowsWithOneGrant(t *testing.T) {
	bad := map[string]RouteDecisionResult{}
	for _, o := range []string{"CLARIFY", "CONFIRM", "WAIT", "ESCALATE", "REJECT", "UNKNOWN", "", "allow"} {
		bad["outcome "+o] = RouteDecisionResult{Outcome: o, DecisionID: "dec_1", GrantIDs: []string{"grt_route1"}}
	}
	bad["no decision id"] = RouteDecisionResult{Outcome: "ALLOW", GrantIDs: []string{"grt_route1"}}
	bad["no grant"] = RouteDecisionResult{Outcome: "ALLOW", DecisionID: "dec_1"}
	bad["two grants"] = RouteDecisionResult{Outcome: "ALLOW", DecisionID: "dec_1", GrantIDs: []string{"grt_a", "grt_b"}}
	bad["empty grant id"] = RouteDecisionResult{Outcome: "ALLOW", DecisionID: "dec_1", GrantIDs: []string{""}}
	bad["not a grant id"] = RouteDecisionResult{Outcome: "ALLOW", DecisionID: "dec_1", GrantIDs: []string{"dec_1"}}
	for name, res := range bad {
		iss, err := newRouteGrantIssuer(&deciderFake{result: res})
		if err != nil {
			t.Fatal(err)
		}
		id, err := iss.IssueRouteGrant(context.Background(), routeReq())
		if err == nil || id != "" || !errors.Is(err, ErrRouteGrantRefused) {
			t.Errorf("%s: id %q, err %v; want no id and ErrRouteGrantRefused", name, id, err)
		}
	}
}

func TestRouteIssuerPassesTrustsErrorOnAndIssuesNothing(t *testing.T) {
	boom := errors.New("synthetic trust failure")
	iss, err := newRouteGrantIssuer(&deciderFake{result: allowOne(), err: boom})
	if err != nil {
		t.Fatal(err)
	}
	id, err := iss.IssueRouteGrant(context.Background(), routeReq())
	if id != "" || !errors.Is(err, boom) {
		t.Fatalf("id %q, err %v; want no id and the error", id, err)
	}
}

// AGENT-08, INV-12: a request that does not name its tenant, correlation, case, version token and queue is refused before Trust is asked.
func TestRouteIssuerRefusesAnIncompleteRequestWithoutAskingTrust(t *testing.T) {
	cuts := map[string]func(*workflows.RouteGrantRequest){
		"tenant":      func(r *workflows.RouteGrantRequest) { r.TenantID = "" },
		"correlation": func(r *workflows.RouteGrantRequest) { r.CorrelationID = "" },
		"case":        func(r *workflows.RouteGrantRequest) { r.CaseID = "" },
		"version":     func(r *workflows.RouteGrantRequest) { r.VersionToken = "" },
		"queue":       func(r *workflows.RouteGrantRequest) { r.PlannedQueue = "" },
	}
	for name, cut := range cuts {
		d := &deciderFake{result: allowOne()}
		iss, _ := newRouteGrantIssuer(d)
		r := routeReq()
		cut(&r)
		if id, err := iss.IssueRouteGrant(context.Background(), r); err == nil || id != "" || len(d.got) != 0 {
			t.Errorf("%s: id %q, err %v, trust calls %d; want a refusal and no call", name, id, err, len(d.got))
		}
	}
}

func TestRouteIssuerNeedsADecider(t *testing.T) {
	if iss, err := newRouteGrantIssuer(nil); err == nil || iss != nil {
		t.Fatalf("issuer %v, err %v; want a refusal", iss, err)
	}
}

// INV-03: the issuer file mints nothing: it imports no signing, grant or trust package and names no Sign call.
func TestRouteIssuerSourceNeverMintsAGrant(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "wire_worker_ports.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, im := range f.Imports {
		p := strings.Trim(im.Path.Value, `"`)
		if strings.Contains(p, "/trust/") && !strings.Contains(p, "/trust/grants") { // the key ring types and the key-file loader are the only trust imports
			t.Errorf("imports %s: the issuer goes through the Trust port only", p)
		}
		if strings.Contains(p, "signing") || strings.Contains(p, "crypto/hmac") {
			t.Errorf("imports %s: nothing here signs", p)
		}
	}
	raw, err := os.ReadFile("wire_worker_ports.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"SignWith(", "NewIssuer(", "grants.Step", "INSERT INTO"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("wire_worker_ports.go contains %q: a grant is minted by Trust only", banned)
		}
	}
}

// ---- the keys and the real ports ----

func putGrantKey(t *testing.T, dir, name string, secret []byte) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(hex.EncodeToString(secret)+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
}

func keyEnv(dir, maxActive string) Env {
	set := map[string]string{"NOEMA_GRANT_KEY_DIR": dir, "NOEMA_GRANT_KEY_MAX_ACTIVE": maxActive}
	return workerEnv(set)
}

func syntheticSecret(b byte) []byte {
	s := make([]byte, 32)
	for i := range s {
		s[i] = b
	}
	return s
}

// SEC-09, AGENT-16: the key ring comes from NOEMA_GRANT_KEY_DIR and NOEMA_GRANT_KEY_MAX_ACTIVE, both required, no default.
func TestGrantKeysComeFromTheKeyDirWithNoDefaults(t *testing.T) {
	dir := t.TempDir()
	putGrantKey(t, dir, "grant.hmac.1.key", syntheticSecret(1))
	if k, err := loadWorkerGrantKeys(keyEnv(dir, "2")); err != nil || k == nil {
		t.Fatalf("keys %v, err %v", k, err)
	}
	for name, env := range map[string]Env{
		"no dir":      keyEnv("", "2"),
		"no max":      keyEnv(dir, ""),
		"max zero":    keyEnv(dir, "0"),
		"max text":    keyEnv(dir, "two"),
		"max too big": keyEnv(dir, "3"),
		"missing dir": keyEnv(filepath.Join(dir, "nope"), "2"),
		"empty dir":   keyEnv(t.TempDir(), "2"),
	} {
		k, err := loadWorkerGrantKeys(env)
		if err == nil || k != nil {
			t.Errorf("%s: keys %v, err %v; want a refusal", name, k, err)
		}
	}
	if _, err := loadWorkerGrantKeys(keyEnv("", "2")); err == nil || !strings.Contains(err.Error(), "NOEMA_GRANT_KEY_DIR") {
		t.Errorf("a missing dir must name the setting, got %v", err)
	}
}

type clockFake struct{ controller.Clock }
type receiptsFake struct{ controller.ReceiptSigner }
type grantsFake struct{ controller.GrantSource }
type bindFake struct{ controller.Binder }
type revalFake struct{ controller.Revalidator }
type ledgerFake struct{ controller.Ledger }
type outFake struct{ controller.Dispatcher }
type idsFake struct{ controller.IDs }
type readerFake struct{ workflows.CaseReader }

func goodDeps() escalateDeps {
	return escalateDeps{Clock: clockFake{}, Receipts: receiptsFake{}, Grants: grantsFake{}, Bind: bindFake{}, Reval: revalFake{}, Ledger: ledgerFake{}, Out: outFake{}, IDs: idsFake{},
		Reader: readerFake{}, Decider: &deciderFake{result: allowOne()}}
}

// REQ-EXE-006, INV-03: the executor is the bridge's over a Controller that verifies grants under the loaded ring; the reader is the supplied case read-back; the issuer is Trust's.
func TestEscalatePortsAreTheRealOnesOverTheLoadedKeys(t *testing.T) {
	dir := t.TempDir()
	secret := syntheticSecret(7)
	putGrantKey(t, dir, "grant.hmac.1.key", secret)
	ports, err := buildEscalatePorts(keyEnv(dir, "2"), goodDeps())
	if err != nil {
		t.Fatal(err)
	}
	ex, ok := ports.Executor.(bridge.GrantExecutor)
	if !ok || ex.Controller == nil {
		t.Fatalf("executor %T, want bridge.GrantExecutor over a Controller", ports.Executor)
	}
	if _, ok := ports.Reader.(readerFake); !ok {
		t.Fatalf("reader %T, want the one supplied (the bridge's case read-back, built where adapters may be imported)", ports.Reader)
	}
	if ports.Issuer == nil {
		t.Fatal("no issuer")
	}
	msg := crypto.GrantSigningMessage([]byte(`{"synthetic":"grant"}`))
	good := crypto.Sign(secret, msg)
	if !ex.Controller.Keys.VerifyWith("grant.hmac.1", msg, good) {
		t.Fatal("a signature under the loaded key was refused")
	}
	if ex.Controller.Keys.VerifyWith("grant.hmac.1", msg, crypto.Sign(syntheticSecret(8), msg)) {
		t.Fatal("a signature under another secret was accepted")
	}
	if ex.Controller.Keys.VerifyWith("grant.hmac.9", msg, good) {
		t.Fatal("a signature under an unknown key id was accepted")
	}
}

// AGENT-16: a missing piece or a bad key setting is an error that names it; no half set of ports comes back.
func TestEscalatePortsRefuseAMissingPieceOrBadKeys(t *testing.T) {
	dir := t.TempDir()
	putGrantKey(t, dir, "grant.hmac.1.key", syntheticSecret(1))
	cuts := map[string]func(*escalateDeps){
		"Clock":    func(d *escalateDeps) { d.Clock = nil },
		"Receipts": func(d *escalateDeps) { d.Receipts = nil },
		"Grants":   func(d *escalateDeps) { d.Grants = nil },
		"Bind":     func(d *escalateDeps) { d.Bind = nil },
		"Reval":    func(d *escalateDeps) { d.Reval = nil },
		"Ledger":   func(d *escalateDeps) { d.Ledger = nil },
		"Out":      func(d *escalateDeps) { d.Out = nil },
		"IDs":      func(d *escalateDeps) { d.IDs = nil },
		"Reader":   func(d *escalateDeps) { d.Reader = nil },
		"Decider":  func(d *escalateDeps) { d.Decider = nil },
	}
	for name, cut := range cuts {
		d := goodDeps()
		cut(&d)
		p, err := buildEscalatePorts(keyEnv(dir, "2"), d)
		if err == nil || !strings.Contains(err.Error(), name) || p.Executor != nil || p.Reader != nil || p.Issuer != nil {
			t.Errorf("%s: ports %+v, err %v; want a refusal naming it", name, p, err)
		}
	}
	if p, err := buildEscalatePorts(keyEnv("", "2"), goodDeps()); err == nil || p.Executor != nil {
		t.Errorf("no key dir: ports %+v, err %v", p, err)
	}
}

// ---- graceful shutdown and readiness ----

type blockingExecutor struct {
	started chan struct{}
	release chan struct{}
	calls   int
}

func (b *blockingExecutor) ExecuteGrant(context.Context, string, string) (workflows.GrantRun, error) {
	b.calls++
	close(b.started)
	<-b.release
	return workflows.GrantRun{}, nil
}

func startBlocked(t *testing.T, env map[string]string) (*workerHandle, *blockingExecutor, *capConn, *[]string, *workflows.EscalateActivities) {
	t.Helper()
	conn, log := newCap(nil)
	calls := 0
	ex := &blockingExecutor{started: make(chan struct{}), release: make(chan struct{})}
	h, err := buildWorker(deadlineCtx(), workerEnv(env), workerPorts{Executor: ex, Reader: &probeReader{}, Issuer: &probeIssuer{}}, dialOf(conn, &calls))
	if err != nil || h == nil {
		t.Fatalf("handle %v, err %v", h, err)
	}
	return h, ex, conn, log, conn.w.acts.(*workflows.EscalateActivities)
}

// REQ-EXE-006, INV-07: Stop lets an in-flight activity finish before the worker is stopped and the connection closed; readiness is false from the moment Stop begins.
func TestStopDrainsInFlightActivitiesBeforeStoppingTheWorker(t *testing.T) {
	h, ex, _, log, acts := startBlocked(t, drainWorker())
	done := make(chan struct{})
	go func() { _, _ = acts.ExecuteGrant(context.Background(), "ten_a", "grt_1"); close(done) }()
	<-ex.started
	stopped := make(chan error, 1)
	go func() { stopped <- h.Stop() }()
	deadline := time.After(5 * time.Second)
	for {
		if ok, why := h.Readiness()(); !ok {
			if !strings.Contains(why, "worker") {
				t.Fatalf("reason %q does not name the worker", why)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("readiness stayed true after Stop began")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case err := <-stopped:
		t.Fatalf("Stop returned (%v) while an activity was in flight", err)
	case <-time.After(20 * time.Millisecond):
	}
	if got := strings.Join(*log, ","); got != "start" {
		t.Fatalf("events %q: the worker was stopped before the activity drained", got)
	}
	close(ex.release)
	if err := <-stopped; err != nil {
		t.Fatalf("Stop: %v, want nil after a full drain", err)
	}
	<-done
	if got := strings.Join(*log, ","); got != "start,stop,close" {
		t.Fatalf("events %q, want start,stop,close", got)
	}
}

// AGENT-16: an activity that does not finish within the bound does not hold the process: Stop reports ErrWorkerDrainTimeout and still stops the worker once.
func TestStopGivesUpAfterTheDrainBound(t *testing.T) {
	env := drainWorker()
	env["NOEMA_TEMPORAL_SHUTDOWN_DRAIN_MS"] = "30"
	h, ex, _, log, acts := startBlocked(t, env)
	go func() { _, _ = acts.ExecuteGrant(context.Background(), "ten_a", "grt_1") }()
	<-ex.started
	if err := h.Stop(); !errors.Is(err, ErrWorkerDrainTimeout) {
		t.Fatalf("Stop: %v, want ErrWorkerDrainTimeout", err)
	}
	if err := h.Stop(); err != nil && !errors.Is(err, ErrWorkerDrainTimeout) {
		t.Fatalf("second Stop: %v", err)
	}
	if got := strings.Join(*log, ","); got != "start,stop,close" {
		t.Fatalf("events %q, want start,stop,close once", got)
	}
	close(ex.release)
}

// AGENT-08, INV-07: after Stop began no new write starts: the port is not called and the activity fails.
func TestNoActivityStartsAfterStop(t *testing.T) {
	h, ex, _, _, acts := startBlocked(t, drainWorker())
	close(ex.release) // a call that wrongly reached the port would return at once and be counted
	if err := h.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := acts.ExecuteGrant(context.Background(), "ten_a", "grt_1"); !errors.Is(err, ErrWorkerStopping) || ex.calls != 0 {
		t.Fatalf("err %v, port calls %d; want ErrWorkerStopping and no call", err, ex.calls)
	}
	if _, err := acts.ReadBackCase(context.Background(), "ten_a", "k"); !errors.Is(err, ErrWorkerStopping) {
		t.Fatalf("read after Stop: %v", err)
	}
	if _, err := acts.IssueRouteGrant(context.Background(), routeReq()); !errors.Is(err, ErrWorkerStopping) {
		t.Fatalf("issue after Stop: %v", err)
	}
}

// AGENT-16: the drain bound is a setting with no default: absent or not a positive whole number refuses the build before a dial.
func TestDrainBoundIsRequiredWhenAWorkerIsConfigured(t *testing.T) {
	for name, v := range map[string]string{"absent": "", "zero": "0", "negative": "-5", "text": "soon"} {
		env := goodWorker()
		if v != "" {
			env["NOEMA_TEMPORAL_SHUTDOWN_DRAIN_MS"] = v
		}
		conn, _ := newCap(nil)
		calls := 0
		ports, _, _, _ := goodPorts()
		h, err := buildWorker(deadlineCtx(), workerEnv(env), ports, dialOf(conn, &calls))
		if h != nil || err == nil || !strings.Contains(err.Error(), "NOEMA_TEMPORAL_SHUTDOWN_DRAIN_MS") || calls != 0 {
			t.Errorf("%s: handle %v, err %v, dials %d", name, h, err, calls)
		}
	}
}

type fatalConn struct {
	*capConn
	fatal chan error
}

func (f *fatalConn) Fatal() <-chan error { return f.fatal }

// REQ-OPS-003: a worker that dies on its own (the connection reports it) turns readiness false, naming the worker.
func TestAWorkerThatDiesOnItsOwnIsNotReady(t *testing.T) {
	cc, _ := newCap(nil)
	fc := &fatalConn{capConn: cc, fatal: make(chan error, 1)}
	calls := 0
	ports, _, _, _ := goodPorts()
	h, err := buildWorker(deadlineCtx(), workerEnv(drainWorker()), ports, dialOf(fc, &calls))
	if err != nil || h == nil {
		t.Fatalf("handle %v, err %v", h, err)
	}
	if ok, _ := h.Readiness()(); !ok {
		t.Fatal("not ready before the failure")
	}
	fc.fatal <- errors.New("synthetic fatal error secret-host:7233")
	deadline := time.After(5 * time.Second)
	for {
		ok, why := h.Readiness()()
		if !ok {
			if !strings.Contains(why, "worker") || strings.Contains(why, "secret-host") {
				t.Fatalf("reason %q must name the worker and carry no cause text", why)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("readiness stayed true after a fatal error")
		case <-time.After(time.Millisecond):
		}
	}
	if err := h.Stop(); err != nil {
		t.Fatal(err)
	}
}
