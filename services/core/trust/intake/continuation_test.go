package intake

// TRU-14 (REQ-TRU-002, REQ-TRU-004, REQ-PLN-002, REQ-TRU-008; INV-03, INV-16, INV-21; A335 and A363, interim, relayed, not yet committed): the continuation evaluation. Trust is
// asked again for ONE next step of an action graph; it runs the same T1 to T15, with the authorized set limited to the runnable step, so that the step has its own decision
// and its own grant. The plan request carries each capability's policy params (the winning PERMIT rule's) and the facts and the evidence packet the params need.
// The tests build the evaluation directly (T1 to T10 continue; T11, T12 and T13 to T15 are real) over fakes of the ports. Every value is SYNTHETIC.

import (
	"context"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/policy"
)

const (
	tCaseID    = "cas_synth0001"
	tQueue     = "queue_synth_ops"
	tTarget    = "txn_synth0001"
	tPacketRef = "evp_synth0001"
)

// runFake is the Runnable port: the capability IDs the graph says may run now.
type runFake struct {
	ids   []string
	err   error
	asked []RunnableQuery
	panic bool
}

func (r *runFake) Runnable(_ context.Context, q RunnableQuery) ([]string, error) {
	r.asked = append(r.asked, q)
	if r.panic {
		panic("injected panic in Runnable.Runnable")
	}
	return append([]string(nil), r.ids...), r.err
}

func permitWith(name string, params map[string]string) PolicyEvaluation {
	p := verdict(name, policy.Permit)
	p.PermitParams = params
	return p
}

// stepFake is the common setup of a continuation of the route step: both graph capabilities authorized, the route step runnable, the policy permitting it with its queue.
func stepFake() (*f13, *capPolicy, *runFake) {
	f := newFake13()
	f.authorized = []string{capCase, capRoute}
	f.plan = writePlan(stepOn(capRoute, tTarget))
	f.rec = RecordResult{DecisionID: "dec_synth0002", GrantIDs: []string{"grt_synth0002"}}
	p := &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("root", policy.Permit), capRoute: permitWith("route", map[string]string{"owner_queue": tQueue})}}
	return f, p, &runFake{ids: []string{capRoute}}
}

func stepEval13(t *testing.T, f *f13, p *capPolicy, r *runFake, cap string) *evaluation {
	t.Helper()
	ev := policyEval13(t, f, p)
	ev.step = &stepScope{capabilityID: cap, facts: map[string]string{"case_id": tCaseID}, packet: tPacketRef, runnable: r}
	return ev
}

// notAllowedAtT11 asserts a continuation that fails closed before anything is asked of the policy, planned or recorded.
func notAllowedAtT11(t *testing.T, name string, f *f13, p *capPolicy, res Result) {
	t.Helper()
	notAllowedAtT11As(t, name, f, p, res, decide.StepFailed)
}

func notAllowedAtT11As(t *testing.T, name string, f *f13, p *capPolicy, res Result, tr decide.TraceResult) {
	t.Helper()
	wantUnknown(t, name, res, "INTERNAL_ERROR", decide.T11Policy, tr)
	if len(p.queries) != 0 || f.called("Planner.Plan") != 0 || f.called("Recorder.Record") != 0 {
		t.Errorf("%s: the policy was asked %v, or the planner or recorder was called: %v", name, p.queried(), f.calls)
	}
}

// ---- the policy params T11 reads reach the Planner ----

// A363: a first evaluation also hands the Planner each capability's winning PERMIT params; Facts and EvidencePacket are for a continuation only; Capabilities is empty.
func TestT14ThePlanRequestCarriesEachCapabilitysPolicyParamsAndNothingElseOnAFirstEvaluation(t *testing.T) {
	f := newFake13()
	planFor(f)
	p := &capPolicy{by: map[string]PolicyEvaluation{capCase: permitWith("root", map[string]string{"priority": "p_synth"}), capRoute: permitWith("graph", map[string]string{"owner_queue": tQueue})}}
	ev := policyEval13(t, f, p)
	res := ev.run(tctx(t), ev.policySteps())
	if res.Decision.Outcome() != decide.Allow || len(f.planReq) != 1 {
		t.Fatalf("%s %v, Plan called %d times; want ALLOW and one Plan", res.Decision.Outcome(), codes13(res.Decision), len(f.planReq))
	}
	got := f.planReq[0]
	want := map[string]map[string]string{capCase: {"priority": "p_synth"}, capRoute: {"owner_queue": tQueue}}
	if !reflect.DeepEqual(got.PolicyParams, want) {
		t.Errorf("PolicyParams %v, want %v", got.PolicyParams, want)
	}
	if len(got.Capabilities) != 0 || got.Facts != nil || got.EvidencePacket != "" {
		t.Errorf("a first evaluation carried capabilities %v, facts %v, packet %q; want none", got.Capabilities, got.Facts, got.EvidencePacket)
	}
}

// A capability whose PERMIT rule has no params has no entry; params of a PERMIT that did not match are never taken (a denied answer carries none, and one that does is dropped).
func TestT11PolicyParamsAreKeptOnlyForAMatchedPermitWithParams(t *testing.T) {
	f := newFake13()
	planFor(f)
	denied := verdict("graph", policy.Reject)
	denied.PermitMatched = false
	denied.PermitParams = map[string]string{"owner_queue": "queue_synth_leak"}
	p := &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("root", policy.Permit), capRoute: denied}}
	ev := policyEval13(t, f, p)
	res := ev.run(tctx(t), ev.policySteps())
	if res.Decision.Outcome() != decide.Reject || len(f.planReq) != 0 {
		t.Fatalf("%s %v, Plan called %d times; want REJECT and no Plan", res.Decision.Outcome(), codes13(res.Decision), len(f.planReq))
	}
	if len(ev.policyParams) != 0 {
		t.Errorf("params were kept: %v (the root had none, the denied capability must not leak any)", ev.policyParams)
	}
}

// ---- a continuation decides ONE step ----

// The happy path: the runnable, authorized, permitted route step is its own decision with its own grant: the policy is asked about it alone, the Planner is asked for it alone with
// its policy param, the case fact and the evidence packet, and one Record writes one grant (T15 unchanged).
func TestContinuationOfTheRouteStepIsItsOwnDecisionAndGrant(t *testing.T) {
	f, p, r := stepFake()
	ev := stepEval13(t, f, p, r, capRoute)
	res := ev.run(tctx(t), ev.policySteps())
	if res.Decision.Outcome() != decide.Allow || res.DecisionID != "dec_synth0002" || !reflect.DeepEqual(res.GrantIDs, []string{"grt_synth0002"}) {
		t.Fatalf("%s %v %q %v, want ALLOW dec_synth0002 [grt_synth0002]", res.Decision.Outcome(), codes13(res.Decision), res.DecisionID, res.GrantIDs)
	}
	if got := p.queried(); !reflect.DeepEqual(got, []string{capRoute}) {
		t.Errorf("the policy was asked about %v, want the step's capability alone", got)
	}
	if len(f.planReq) != 1 {
		t.Fatalf("Plan called %d times, want once", len(f.planReq))
	}
	pr := f.planReq[0]
	if !reflect.DeepEqual(pr.Capabilities, []string{capRoute}) || pr.Facts["case_id"] != tCaseID || pr.EvidencePacket != tPacketRef || !pr.WriteRequired {
		t.Errorf("the plan request %+v, want capability [%s], fact case_id %s, packet %s, write", pr, capRoute, tCaseID, tPacketRef)
	}
	if want := map[string]map[string]string{capRoute: {"owner_queue": tQueue}}; !reflect.DeepEqual(pr.PolicyParams, want) {
		t.Errorf("PolicyParams %v, want %v (the step's capability only)", pr.PolicyParams, want)
	}
	if f.called("Recorder.Record") != 1 || len(rec0(t, f).Steps) != 1 || rec0(t, f).Steps[0].CapabilityID != capRoute {
		t.Errorf("Record called %d times with steps %v, want one Record of the one step", f.called("Recorder.Record"), f.recReqs)
	}
	if len(r.asked) != 1 || r.asked[0].Facts["case_id"] != tCaseID || r.asked[0].IntentNode != "fin.payments.exception.investigate" || r.asked[0].TenantID != t13Tenant {
		t.Errorf("the runnable port was asked %+v, want once, for the intent node, with the facts", r.asked)
	}
}

// The runnable set is read once per evaluation, whichever step asks first (T11, T12 and T14 share it).
func TestContinuationReadsTheRunnableSetOnce(t *testing.T) {
	f, p, r := stepFake()
	ev := stepEval13(t, f, p, r, capRoute)
	ev.run(tctx(t), ev.policySteps())
	if len(r.asked) != 1 {
		t.Errorf("the runnable port was asked %d times, want once", len(r.asked))
	}
}

// INV-03: a capability the graph says is not runnable now (its preconditions are not met) is never decided: fail closed at T11, before the policy, the planner or the recorder.
func TestContinuationOfAStepThatIsNotRunnableFailsClosed(t *testing.T) {
	f, p, r := stepFake()
	r.ids = []string{capCase} // another step is runnable; the asked one is not
	notAllowedAtT11(t, "a step that is not runnable", f, p, run13step(t, f, p, r, capRoute))
}

func TestContinuationWithNothingRunnableFailsClosed(t *testing.T) {
	f, p, r := stepFake()
	r.ids = nil
	notAllowedAtT11(t, "an empty runnable set", f, p, run13step(t, f, p, r, capRoute))
}

// The runnable set cannot be read: not "none" and not "all". A panic is the same (the runner turns it into a failed step).
func TestContinuationWhoseRunnableSetCannotBeReadFailsClosed(t *testing.T) {
	f, p, r := stepFake()
	r.err = errInjected
	notAllowedAtT11(t, "a runnable error", f, p, run13step(t, f, p, r, capRoute))
	f, p, r = stepFake()
	r.panic = true
	notAllowedAtT11As(t, "a runnable panic", f, p, run13step(t, f, p, r, capRoute), decide.StepPanicked)
}

// A runnable step the intent and the tenant's policy do not authorize (outside Capabilities.Authorized) is never decided, whatever the graph says.
func TestContinuationOfARunnableStepOutsideTheAuthorizedSetFailsClosed(t *testing.T) {
	f, p, r := stepFake()
	f.authorized = []string{capCase}
	notAllowedAtT11(t, "a step outside the authorized set", f, p, run13step(t, f, p, r, capRoute))
}

// An authorized set that cannot be read is an error, as for a first evaluation.
func TestContinuationWhoseAuthorizedSetCannotBeReadFailsClosed(t *testing.T) {
	f, p, r := stepFake()
	f.authErr = errInjected
	notAllowedAtT11(t, "an unreadable authorized set", f, p, run13step(t, f, p, r, capRoute))
}

func run13step(t *testing.T, f *f13, p *capPolicy, r *runFake, cap string) Result {
	t.Helper()
	ev := stepEval13(t, f, p, r, cap)
	return ev.run(tctx(t), ev.policySteps())
}

// The policy's answer for the step is the step's alone: a REJECT of the route step ends at T11 (the rules of the first step are not asked and cannot make up for it).
func TestContinuationIsRefusedByThePolicyOfThatStepAlone(t *testing.T) {
	f, p, r := stepFake()
	p.by[capRoute] = verdict("route", policy.Reject)
	res := run13step(t, f, p, r, capRoute)
	if res.Decision.Outcome() != decide.Reject || lastTrace(res).Step != decide.T11Policy || f.called("Planner.Plan") != 0 || f.called("Recorder.Record") != 0 {
		t.Errorf("%s %v, trace %v, calls %v; want REJECT at T11 and nothing planned or recorded", res.Decision.Outcome(), codes13(res.Decision), res.Trace, f.calls)
	}
	if got := p.queried(); !reflect.DeepEqual(got, []string{capRoute}) {
		t.Errorf("the policy was asked about %v, want the step's capability alone", got)
	}
}

// T13 as noema#759 built it (A435): a CONFIRM the profile asks for stops the continuation with CONFIRM, and the CONFIRM is a decision, so it is recorded once, first, naming the open
// request; the continuation plans nothing at T13 when the request is open (it asks the Planner only to create a new one) and no grant comes from a CONFIRM.
func TestContinuationNeedingAConfirmationStopsAtT13(t *testing.T) {
	f, p, r := stepFake()
	f.conf = Confirmation{Status: ConfirmationPending, RequestID: "cfr_synth0001"}
	f.rec = RecordResult{DecisionID: "dec_synth0003"}
	res := run13step(t, f, p, r, capRoute)
	if res.Decision.Outcome() != decide.Confirm || lastTrace(res).Step != decide.T13Confirmation || f.called("Recorder.Record") != 1 || f.called("Planner.Plan") != 0 {
		t.Errorf("%s %v, calls %v; want CONFIRM at T13, recorded once and nothing planned", res.Decision.Outcome(), codes13(res.Decision), f.calls)
	}
	if res.DecisionID != "dec_synth0003" || res.ConfirmationRequestID != "cfr_synth0001" || len(res.GrantIDs) != 0 {
		t.Errorf("decision %q, request %q, grants %v; want the recorded CONFIRM naming the open request and no grant", res.DecisionID, res.ConfirmationRequestID, res.GrantIDs)
	}
}

// ---- T12: a step whose dispatch budget is spent ----

func (e *evaluation) stepSteps() decide.Steps {
	s := e.policySteps()
	s[decide.T12Clocks-1] = e.t12
	return s
}

// A spent budget of the step's capability escalates DISPATCH_BUDGET_EXHAUSTED at T12 (A224): no grant. The ledger is asked about the step's capability alone, never about the
// first step's, so a spent budget of the case creation does not block the route and the route's own cannot be skipped.
func TestContinuationOfAStepWhoseDispatchBudgetIsSpentEscalatesAndIsNotGranted(t *testing.T) {
	f, p, r := stepFake()
	f.dispatch = DispatchCount{Used: 1, Limit: 1}
	f.rec = RecordResult{DecisionID: "dec_synth0004"} // an ESCALATE is recorded with no step and no grant (A534)
	ev := stepEval13(t, f, p, r, capRoute)
	ev.svc.Ports.Clocks = p.fake
	res := ev.run(tctx(t), ev.stepSteps())
	if res.Decision.Outcome() != decide.Escalate || !reflect.DeepEqual(codes13(res.Decision), []string{"DISPATCH_BUDGET_EXHAUSTED"}) || lastTrace(res).Step != decide.T12Clocks {
		t.Fatalf("%s %v, trace %v; want ESCALATE DISPATCH_BUDGET_EXHAUSTED at T12", res.Decision.Outcome(), codes13(res.Decision), res.Trace)
	}
	if len(f.recReqs) != 1 || len(f.recReqs[0].Steps) != 0 || f.called("Planner.Plan") != 0 || res.DecisionID != "dec_synth0004" || len(res.GrantIDs) != 0 {
		t.Errorf("after a spent budget: %v %q %v; want the ESCALATE recorded once with no step, no plan asked, no grant", f.calls, res.DecisionID, res.GrantIDs)
	}
	if len(f.dispatchKey) != 1 || f.dispatchKey[0].CapabilityID != capRoute || f.dispatchKey[0].TargetRef != tTarget {
		t.Errorf("the ledger was asked %+v, want the step's capability on the intent's target, once", f.dispatchKey)
	}
}

// ---- T14: the plan of a continuation is that one step ----

// A planner that answers with more than the one step is refused: one grant for each call (A335).
func TestContinuationWhosePlanHasMoreThanTheOneStepIsPlanInvalid(t *testing.T) {
	f, p, r := stepFake()
	f.plan = writePlan(stepOn(capRoute, tTarget), stepOn(capCase, tTarget))
	f.rec = RecordResult{DecisionID: "dec_synth0002", GrantIDs: []string{"grt_synth0002", "grt_synth0003"}}
	planRefused(t, "a two step plan", f, run13step(t, f, p, r, capRoute))
}

// A planner that substitutes another capability for the one asked is refused, even when that capability is authorized and permitted.
func TestContinuationWhosePlanIsAnotherCapabilityIsPlanInvalid(t *testing.T) {
	f, p, r := stepFake()
	f.plan = writePlan(stepOn(capCase, tTarget))
	planRefused(t, "a substituted capability", f, run13step(t, f, p, r, capRoute))
}

// A plan with no step for a write is not well formed (unchanged), here for a continuation.
func TestContinuationWhosePlanIsEmptyIsPlanInvalid(t *testing.T) {
	f, p, r := stepFake()
	f.plan = writePlan()
	planRefused(t, "an empty plan", f, run13step(t, f, p, r, capRoute))
}

// The step a continuation request names carries the capability, the facts (copied, so the caller cannot change them mid-evaluation), the evidence packet and the port.
func TestScopeOfCarriesTheRequestsStepFactsAndPacket(t *testing.T) {
	facts := map[string]string{"case_id": tCaseID}
	run := &runFake{}
	st := scopeOf(ContinuationRequest{CapabilityID: capRoute, Facts: facts, EvidencePacket: tPacketRef, Runnable: run})
	facts["case_id"] = "cas_changed"
	if st.capabilityID != capRoute || st.packet != tPacketRef || st.runnable != Runnable(run) || !reflect.DeepEqual(st.facts, map[string]string{"case_id": tCaseID}) {
		t.Errorf("scope %+v, want %s, packet %s, the port and a copy of the facts", st, capRoute, tPacketRef)
	}
}
