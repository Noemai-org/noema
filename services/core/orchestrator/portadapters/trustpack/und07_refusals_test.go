package trustpack_test

// UND-07 (REQ-INT-005, REQ-CAP-002; INV-05, INV-16, INV-18, BD-08; US-10, US-22, PUS-03). Every refusal below runs the REAL intake (T1 to T15) over the REAL pack adapters
// (Capabilities, Scope, Authority of the shipped fintech pack) and proves what the PERD asks of an out-of-scope or prohibited request: the rejection comes from the registry,
// never from the model; it is REJECT with the one core reason code the step owns; NOTHING about a target is read before it (no candidates, ownership, evidence, reconciliation,
// policy, clock, confirmation, plan or dispatch call); no grant exists (INV-03) and, if the intake records the refusal, the one record has no step and no plan hash; and the request ends there.
//
// The ports that read about a target and the Recorder are counting doubles (they are what the evaluation must NOT reach, or must reach exactly once); the adapters under test are
// the real ones, and every expectation is read from the pack files by the independent reader of helpers_test.go. SYNTHETIC data only (AGENT-25).

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const (
	und07Principal   = "usr_synth01"
	und07Correlation = "corr_synth01"
	und07DecisionID  = "dec_synth0001"
)

// refusalWorld is the stored intent the intake is asked about, the real adapters, and the doubles. calls names every double method that was reached, in order.
type refusalWorld struct {
	t          *testing.T
	node       string
	requested  string // the capability the stored intent asks for; empty for none
	needsWrite bool
	caps       trustpack.Capabilities
	scope      trustpack.Scope
	authority  trustpack.Authority

	calls    []string
	recorded []intake.RecordRequest
}

func newRefusalWorld(t *testing.T, node, requested string, needsWrite bool) *refusalWorld {
	t.Helper()
	p := loadReal(t)
	caps, err := trustpack.NewCapabilities(p, escalateGraph(t).ID)
	if err != nil {
		t.Fatalf("NewCapabilities: %v", err)
	}
	return &refusalWorld{t: t, node: node, requested: requested, needsWrite: needsWrite, caps: caps, scope: newScope(t, p), authority: newAuthority(t, p)}
}

func (w *refusalWorld) hit(name string) { w.calls = append(w.calls, name) }

// T1: the stored intent, its hash and the session's principal.
func (w *refusalWorld) Get(context.Context, string, string, int) (intake.AdmittedIntent, error) {
	return intake.AdmittedIntent{TenantID: tenant, IntentID: intentIDForTest, Version: 1, IntentNode: w.node, IntentHash: "sha256:" + hex64ForTest(), CorrelationID: und07Correlation,
		PrincipalID: und07Principal, RequestedCapability: w.requested}, nil
}
func (w *refusalWorld) LatestVersion(context.Context, string, string) (int, error) { return 1, nil }
func (w *refusalWorld) Verify(context.Context, intake.AdmittedIntent) error        { return nil }

// T3
func (w *refusalWorld) NeedsWrite(context.Context, intake.AdmittedIntent) (bool, error) {
	return w.needsWrite, nil
}
func (w *refusalWorld) Enabled(context.Context, string) (bool, error) { return true, nil }

// T4 to T6 are the REAL adapters of the shipped pack.
func (w *refusalWorld) Requested(ctx context.Context, a intake.AdmittedIntent) (intake.Capability, bool, error) {
	return w.caps.Requested(ctx, a)
}
func (w *refusalWorld) Check(ctx context.Context, id, role string) (intake.Capability, error) {
	return w.caps.Check(ctx, id, role)
}
func (w *refusalWorld) Authorized(ctx context.Context, a intake.AdmittedIntent) ([]string, error) {
	return w.caps.Authorized(ctx, a)
}
func (w *refusalWorld) InScope(ctx context.Context, tenantID, node string) (bool, error) {
	return w.scope.InScope(ctx, tenantID, node)
}
func (w *refusalWorld) RoleAllowed(ctx context.Context, node, role string) (bool, error) {
	return w.authority.RoleAllowed(ctx, node, role)
}

// Everything that reads about a target, and everything after the gates, must NOT be reached: each double records the call and answers nothing useful.
func (w *refusalWorld) ScopedCandidates(context.Context, string, string, intake.AdmittedIntent) (intake.TargetCandidates, error) {
	w.hit("Candidates.ScopedCandidates")
	return intake.TargetCandidates{}, nil
}
func (w *refusalWorld) OwnsTarget(context.Context, string, string, intake.AdmittedIntent) (bool, error) {
	w.hit("Ownership.OwnsTarget")
	return false, nil
}
func (w *refusalWorld) SnapshotFreshness(context.Context, string, string) (intake.SnapshotEvidence, error) {
	w.hit("Evidence.SnapshotFreshness")
	return intake.SnapshotEvidence{}, nil
}
func (w *refusalWorld) Reconcile(context.Context, string, string) (intake.Reconciliation, error) {
	w.hit("Reconciler.Reconcile")
	return intake.Reconciliation{}, nil
}
func (w *refusalWorld) Evaluate(context.Context, intake.PolicyQuery) (intake.PolicyEvaluation, error) {
	w.hit("Policies.Evaluate")
	return intake.PolicyEvaluation{}, nil
}
func (w *refusalWorld) Applicable(context.Context, intake.ClockQuery) (intake.ClockSet, error) {
	w.hit("Clocks.Applicable")
	return intake.ClockSet{}, nil
}
func (w *refusalWorld) Route(context.Context, intake.ClockQuery, []clocks.Status) (intake.ClockRouting, error) {
	w.hit("Clocks.Route")
	return intake.ClockRouting{}, nil
}
func (w *refusalWorld) Status(context.Context, intake.AdmittedIntent, intake.FreshBinding) (intake.Confirmation, error) {
	w.hit("Confirmations.Status")
	return intake.Confirmation{}, nil
}
func (w *refusalWorld) Plan(context.Context, intake.PlanRequest) (intake.Plan, error) {
	w.hit("Planner.Plan")
	return intake.Plan{}, nil
}
func (w *refusalWorld) Count(context.Context, intake.DispatchKey) (intake.DispatchCount, error) {
	w.hit("Dispatches.Count")
	return intake.DispatchCount{}, nil
}

// The Recorder is reached exactly once, with the refusal: the Decision Record first (PERD 32.4), for every outcome (A456).
func (w *refusalWorld) Record(_ context.Context, r intake.RecordRequest) (intake.RecordResult, error) {
	w.hit("Recorder.Record")
	w.recorded = append(w.recorded, r)
	return intake.RecordResult{DecisionID: und07DecisionID}, nil
}

func (w *refusalWorld) service() *intake.Service {
	return &intake.Service{
		Ports: intake.Ports{Intents: w, Writes: w, Capabilities: w, Scope: w, Authority: w, Candidates: w, Ownership: w, Evidence: w, Reconciler: w, Policies: w, Clocks: w,
			Confirmations: w, Planner: w, Recorder: w, Dispatches: w},
		Clock:       clock.NewManual(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)), // SYNTHETIC
		PackVersion: "1.0.0",
	}
}

func (w *refusalWorld) evaluate(role string) intake.Result {
	w.t.Helper()
	res, err := w.service().Evaluate(ctx(w.t), intake.Request{Session: intake.Session{TenantID: tenant, PrincipalID: und07Principal, Role: role}, CorrelationID: und07Correlation,
		IntentRef: intentIDForTest + "@1", EvidenceSnapshotID: "evs_synth0001"})
	if err != nil {
		w.t.Fatalf("Evaluate: %v", err)
	}
	return res
}

func hex64ForTest() string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = "0123456789abcdef"[i%16]
	}
	return string(b)
}

func reasonsOf(d decide.Decision) []string {
	var out []string
	for _, r := range d.Reasons() {
		out = append(out, r.String())
	}
	return out
}

// requireRefusedBeforeAnyRead asserts the whole of what UND-07 owes for one request.
func requireRefusedBeforeAnyRead(t *testing.T, name string, w *refusalWorld, res intake.Result, reason string, step decide.GateStep) {
	t.Helper()
	if got := res.Decision.Outcome(); got != decide.Reject {
		t.Errorf("%s: outcome %s, want REJECT", name, got)
	}
	if got := reasonsOf(res.Decision); !reflect.DeepEqual(got, []string{reason}) {
		t.Errorf("%s: reasons %v, want [%s]", name, got, reason)
	}
	if n := len(res.Trace); n == 0 || res.Trace[n-1].Step != step || res.Trace[n-1].Result != decide.StepTerminal {
		t.Errorf("%s: trace %v, want it to end terminal at %v", name, res.Trace, step)
	}
	// nothing about a target was read, nothing was planned, nothing was counted: no double is reached except, at most, the Recorder (the intake of this head does not record a
	// refusal of T1 to T9 yet: A581/A574 are C1's; the assertion holds before and after that change).
	for _, c := range w.calls {
		if c != "Recorder.Record" {
			t.Errorf("%s: the evaluation reached %v, want no read of a target and no plan before the refusal", name, w.calls)
			break
		}
	}
	if len(w.recorded) > 1 {
		t.Fatalf("%s: %d Decision Records, want at most one", name, len(w.recorded))
	}
	for _, r := range w.recorded {
		if r.Decision.Outcome() != decide.Reject || len(r.Steps) != 0 || r.PlanHash != "" || r.ConfirmationRequest != nil {
			t.Errorf("%s: the record is %s with %d steps, plan %q, confirmation %v; want a REJECT with none", name, r.Decision.Outcome(), len(r.Steps), r.PlanHash, r.ConfirmationRequest)
		}
	}
	if len(res.GrantIDs) != 0 || res.ConfirmationRequestID != "" {
		t.Errorf("%s: grants %v, confirmation %q; want no grant and no request (INV-03)", name, res.GrantIDs, res.ConfirmationRequestID)
	}
}

// US-10, US-22, REQ-INT-005: every prohibited capability of the shipped pack, asked by name by an intent of the investigation node, for each role the node allows, is REJECT
// PROHIBITED_CAPABILITY at T4 and nothing is read (B6; PERD 20.1: the rejection is the registry's).
func TestEveryProhibitedCapabilityOfThePackIsRefusedAtT4BeforeAnyRead(t *testing.T) {
	node := firstNodeWithProfile(t).ID
	seen := 0
	for _, c := range readRawPack(t).Caps {
		if !c.Prohibited {
			continue
		}
		seen++
		for _, role := range []string{"customer", "ops", "audit", ""} {
			w := newRefusalWorld(t, node, c.ID, true)
			res := w.evaluate(role)
			requireRefusedBeforeAnyRead(t, c.ID+" as "+role, w, res, "PROHIBITED_CAPABILITY", decide.T4Capability)
		}
	}
	if seen == 0 {
		t.Fatal("the pack holds no prohibited capability: the test proves nothing")
	}
}

// INV-18, BD-08: a capability the pack's registry does not know has no adapter and is refused the same way, whatever its name suggests.
func TestACapabilityTheRegistryDoesNotKnowIsRefusedAtT4BeforeAnyRead(t *testing.T) {
	for _, id := range []string{"fin.cap.buy_airtime", "fin.cap.send_money", "x.unknown.cap", "FIN.CAP.TRANSFER_FUNDS", " fin.cap.transfer_funds", "fin.cap.transfer_funds "} {
		w := newRefusalWorld(t, firstNodeWithProfile(t).ID, id, true)
		res := w.evaluate("customer")
		requireRefusedBeforeAnyRead(t, id, w, res, "PROHIBITED_CAPABILITY", decide.T4Capability)
	}
}

// PUS-03, REQ-INT-005: an intent of the out-of-scope node (no trust profile) is REJECT OUT_OF_SCOPE at T5 and nothing is read; a node the pack does not know is refused the same way.
func TestAnOutOfScopeNodeIsRefusedAtT5BeforeAnyRead(t *testing.T) {
	for _, node := range []string{nodeWithoutProfile(t).ID, "x.unknown.node", ""} {
		for _, role := range []string{"customer", "ops"} {
			w := newRefusalWorld(t, node, "", false)
			res := w.evaluate(role)
			requireRefusedBeforeAnyRead(t, "node "+node+" as "+role, w, res, "OUT_OF_SCOPE", decide.T5Scope)
		}
	}
}

// T4 comes before T5: a prohibited remedy named on an out-of-scope node is the prohibited refusal, and still nothing is read.
func TestAProhibitedRemedyOnAnOutOfScopeNodeIsTheProhibitedRefusal(t *testing.T) {
	for _, c := range readRawPack(t).Caps {
		if !c.Prohibited {
			continue
		}
		w := newRefusalWorld(t, nodeWithoutProfile(t).ID, c.ID, true)
		res := w.evaluate("customer")
		requireRefusedBeforeAnyRead(t, c.ID+" on the out-of-scope node", w, res, "PROHIBITED_CAPABILITY", decide.T4Capability)
	}
}

// The control: the same real adapters let an allowed capability of the investigation node past T4 and T5, so the refusals above are the refusals of the pack's data and not of a
// setup that refuses everything. (The evaluation then goes on to T7 and reads about the target, which this test's doubles record.)
func TestAnAllowedCapabilityIsNotRefusedAtT4OrT5(t *testing.T) {
	var allowed string
	for _, c := range readRawPack(t).Caps {
		if !c.Prohibited && len(c.Authority.Roles) > 0 {
			allowed = c.ID
			break
		}
	}
	if allowed == "" {
		t.Fatal("the pack holds no allowed capability")
	}
	w := newRefusalWorld(t, firstNodeWithProfile(t).ID, allowed, false)
	res := w.evaluate("customer")
	for _, r := range reasonsOf(res.Decision) {
		if r == "PROHIBITED_CAPABILITY" || r == "OUT_OF_SCOPE" {
			t.Fatalf("an allowed capability was refused with %s: trace %v", r, res.Trace)
		}
	}
	if n := len(res.Trace); n == 0 || res.Trace[n-1].Step < decide.T6TechnicalAuthority {
		t.Fatalf("the evaluation stopped before T6: %v", res.Trace)
	}
}
