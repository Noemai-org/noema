package intake

// TRU-07 (PR 2 of the confirmation lanes; REQ-TRU-009, REQ-TRU-008, INV-25, INV-03, INV-12, AGENT-16, EC-E16; SoT 43.2a): T13 records the CONFIRM it returns, creates the confirmation
// request when none is open, and never returns a CONFIRM the Recorder did not write. Declined is NOT built here: PERD 23.3 gives T13 the terminal result CONFIRM only (stop condition,
// status/TRU-07-confirm-t13.md); its interim test is TestT13DeclinedIsUnknownForNowAndNothingIsPlannedOrRecorded. All data is SYNTHETIC.

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

const (
	tRequestID = "cfr_synth0001"
	tTTL       = 10 * time.Minute
)

// confirmEval is an evaluation at T13 with what T9 and T11 learned: the evidence set hash and one policy version, and the evaluation's clock reading.
func confirmEval(t *testing.T, f *f13) *evaluation {
	t.Helper()
	ev := newEval13(t, f)
	ev.now = ev.svc.Clock.Now().Unix()
	ev.evidenceSetHash = tSetHash
	ev.policyVersions = []decide.PolicyVersionRef{{PolicyID: "pol_synth01", Version: "3"}}
	return ev
}

func pendingNew(f *f13) *f13 {
	f.conf = Confirmation{Status: ConfirmationPending, NewRequest: &RequestSpec{Requirement: "SELF", TTL: tTTL}}
	f.rec = RecordResult{DecisionID: "dec_synth0002", ConfirmationRequestID: "cfr_synth0002"}
	return f
}

func pendingOpen(f *f13) *f13 {
	f.conf = Confirmation{Status: ConfirmationPending, RequestID: tRequestID}
	f.rec = RecordResult{DecisionID: "dec_synth0002"}
	return f
}

// wantRecordedNothing fails when anything was recorded.
func wantRecordedNothing(t *testing.T, name string, f *f13) {
	t.Helper()
	if f.called("Recorder.Record") != 0 {
		t.Errorf("%s: the Recorder was called: %v", name, f.calls)
	}
}

// REQ-TRU-009, INV-25, INV-12, EC-E16: Pending with a request to make: T13 asks the Planner for the plan, then records the CONFIRM and the one request, bound to the eight values of
// SoT 43.2a, in a single Record call, and only then returns the CONFIRM. The requester is the admitted intent's principal; the expiry is the evaluation's clock plus the spec's TTL.
func TestT13PendingWithANewRequestPlansThenRecordsOneConfirmAndOneRequest(t *testing.T) {
	f := pendingNew(newFake13())
	ev := confirmEval(t, f)
	res := run13(t, ev)
	d := res.Decision
	if d.Outcome() != decide.Confirm {
		t.Fatalf("outcome %s %v, want CONFIRM", d.Outcome(), codes13(d))
	}
	if got := lastTrace(res); got.Step != decide.T13Confirmation || got.Result != decide.StepTerminal {
		t.Errorf("trace ends %v, want T13 terminal", got)
	}
	if !reflect.DeepEqual(f.calls, []string{"Confirmations.Status", "Planner.Plan", "Recorder.Record"}) {
		t.Errorf("calls %v, want Status, then the Planner, then one Record", f.calls)
	}
	wantPlan := PlanRequest{TenantID: t13Tenant, CorrelationID: "corr_synth0001", IntentID: tIntent, IntentVersion: t13Version, IntentHash: tHash, IntentNode: "fin.payments.exception.investigate", WriteRequired: true}
	if len(f.planReq) != 1 || f.planReq[0] != wantPlan {
		t.Errorf("plan request %+v, want %+v", f.planReq, wantPlan)
	}
	r := rec0(t, f)
	want := &NewConfirmationRequest{PrincipalID: t13Principal, IntentHash: tHash, CapabilityID: "fin.cap.create_escalation_case", TargetRef: "txn_synth0001", ParamsHash: tHash,
		EvidenceSetHash: tSetHash, PlanHash: tPlanHash, PolicyVersion: "pol_synth01@3", Requirement: "SELF", RequesterID: t13Principal, ExpiresAt: ev.now + int64(tTTL/time.Second)}
	if !reflect.DeepEqual(r.ConfirmationRequest, want) {
		t.Errorf("the request recorded is %+v, want %+v", r.ConfirmationRequest, want)
	}
	if r.Decision.Outcome() != decide.Confirm || !r.Decision.Authority().AllPass() {
		t.Errorf("the decision recorded is %s %v, want a CONFIRM with all three layers PASS", r.Decision.Outcome(), r.Decision.Authority())
	}
	if r.Evaluated == nil || r.BasisDigest == "" || ev.bb.Check(r.Decision) != nil {
		t.Errorf("the CONFIRM does not rest on the evaluated set of the builder")
	}
	if len(r.Steps) != 0 || r.ConfirmationEventID != "" || r.ConfirmationPlanHash != "" {
		t.Errorf("a CONFIRM was recorded with steps %v or a confirmation event %q %q: no grant can follow a CONFIRM", r.Steps, r.ConfirmationEventID, r.ConfirmationPlanHash)
	}
	if r.TenantID != t13Tenant || r.CorrelationID != "corr_synth0001" || r.PrincipalID != t13Principal || r.IntentID != tIntent || r.IntentHash != tHash || r.IntentVersion != t13Version {
		t.Errorf("the identifiers of the record request are %+v", r)
	}
	if res.DecisionID != "dec_synth0002" || res.ConfirmationRequestID != "cfr_synth0002" || len(res.GrantIDs) != 0 {
		t.Errorf("the result names decision %q, request %q, grants %v; want dec_synth0002, cfr_synth0002, none", res.DecisionID, res.ConfirmationRequestID, res.GrantIDs)
	}
}

// REQ-TRU-009, INV-25: Pending with a request already open: the CONFIRM is recorded and NO second request is made; the Planner is not asked.
func TestT13PendingWithAnOpenRequestRecordsTheConfirmAndCreatesNoRequest(t *testing.T) {
	f := pendingOpen(newFake13())
	res := run13(t, confirmEval(t, f))
	if res.Decision.Outcome() != decide.Confirm {
		t.Fatalf("outcome %s %v, want CONFIRM", res.Decision.Outcome(), codes13(res.Decision))
	}
	if f.called("Planner.Plan") != 0 || f.called("Recorder.Record") != 1 {
		t.Errorf("calls %v, want one Record and no plan", f.calls)
	}
	r := rec0(t, f)
	if r.ConfirmationRequest != nil {
		t.Errorf("a request was asked for although one is open: %+v", r.ConfirmationRequest)
	}
	if r.Decision.Outcome() != decide.Confirm || len(r.Steps) != 0 {
		t.Errorf("recorded %s with %d steps", r.Decision.Outcome(), len(r.Steps))
	}
	if res.DecisionID != "dec_synth0002" || res.ConfirmationRequestID != tRequestID {
		t.Errorf("the result names decision %q and request %q, want dec_synth0002 and the open %s", res.DecisionID, res.ConfirmationRequestID, tRequestID)
	}
}

// openOnce is the Confirmations store and the Recorder as one world: the first CONFIRM that carries a request opens it, and from then on Status says Pending with that request.
type openOnce struct {
	*f13
	open    string
	created int
}

func (o *openOnce) Status(ctx2 context.Context, a AdmittedIntent, b FreshBinding) (Confirmation, error) {
	_, err := o.f13.Status(ctx2, a, b)
	if o.open != "" {
		return Confirmation{Status: ConfirmationPending, RequestID: o.open}, err
	}
	return Confirmation{Status: ConfirmationPending, NewRequest: &RequestSpec{Requirement: "SELF", TTL: tTTL}}, err
}

func (o *openOnce) Record(ctx2 context.Context, r RecordRequest) (RecordResult, error) {
	_, err := o.f13.Record(ctx2, r)
	if r.ConfirmationRequest != nil {
		o.created++
		o.open = "cfr_synth0003"
		return RecordResult{DecisionID: "dec_synth0003", ConfirmationRequestID: o.open}, err
	}
	return RecordResult{DecisionID: "dec_synth0004"}, err
}

// REQ-TRU-009, INV-25 (re-evaluation): evaluating the same intent again while a request is open and unexpired creates no second request. Exactly one request across the two
// evaluations, and the second CONFIRM names the one that is open.
func TestT13ReEvaluationWhileARequestIsOpenCreatesNoSecondRequest(t *testing.T) {
	f := newFake13()
	world := &openOnce{f13: f}
	var first, second Result
	for i, res := range []*Result{&first, &second} {
		ev := confirmEval(t, f)
		ev.svc.Ports.Confirmations, ev.svc.Ports.Recorder = world, world
		*res = run13(t, ev)
		if res.Decision.Outcome() != decide.Confirm {
			t.Fatalf("evaluation %d: %s %v, want CONFIRM", i+1, res.Decision.Outcome(), codes13(res.Decision))
		}
	}
	if world.created != 1 {
		t.Errorf("%d requests were created over two evaluations, want exactly 1", world.created)
	}
	if first.ConfirmationRequestID != "cfr_synth0003" || second.ConfirmationRequestID != "cfr_synth0003" {
		t.Errorf("the evaluations name requests %q and %q, want the one open request twice", first.ConfirmationRequestID, second.ConfirmationRequestID)
	}
	if len(f.recReqs) != 2 || f.recReqs[1].ConfirmationRequest != nil {
		t.Errorf("the second Record %+v carried a request", f.recReqs)
	}
}

// A Pending answer is either "a request is open" or "make one": with both, with neither, or with an ID that is not a request ID it is not an answer (fail closed): UNKNOWN, nothing
// planned for it and nothing recorded.
func TestT13PendingThatIsNotAnAnswerIsUnknownAndNothingIsRecorded(t *testing.T) {
	spec := &RequestSpec{Requirement: "SELF", TTL: tTTL}
	for name, c := range map[string]Confirmation{
		"neither a request ID nor a request to make": {Status: ConfirmationPending},
		"both a request ID and a request to make":    {Status: ConfirmationPending, RequestID: tRequestID, NewRequest: spec},
		"a request ID of another kind":               {Status: ConfirmationPending, RequestID: "cfe_synth0001"},
		"a request ID with no body":                  {Status: ConfirmationPending, RequestID: "cfr_"},
		"a request ID with a space":                  {Status: ConfirmationPending, RequestID: "cfr_synth 0001"},
	} {
		f := newFake13()
		f.conf = c
		res := run13(t, confirmEval(t, f))
		wantUnknown(t, name, res, "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
		wantRecordedNothing(t, name, f)
		if f.called("Planner.Plan") != 0 {
			t.Errorf("%s: the Planner was asked", name)
		}
	}
}

// NotRequired carries no request; Given needs the request it answers and no request to make. Anything else is not an answer.
func TestT13NotRequiredAndGivenThatCarryTheWrongFieldsAreUnknown(t *testing.T) {
	spec := &RequestSpec{Requirement: "SELF", TTL: tTTL}
	for name, c := range map[string]Confirmation{
		"NotRequired with a request ID":      {Status: ConfirmationNotRequired, RequestID: tRequestID},
		"NotRequired with a request to make": {Status: ConfirmationNotRequired, NewRequest: spec},
		"Given with no request ID":           {Status: ConfirmationGiven, EventID: "cfe_synth0001", PlanHash: tPlanHash},
		"Given with a malformed request ID":  {Status: ConfirmationGiven, EventID: "cfe_synth0001", PlanHash: tPlanHash, RequestID: "cfe_synth0001"},
		"Given with a request to make":       {Status: ConfirmationGiven, EventID: "cfe_synth0001", PlanHash: tPlanHash, RequestID: tRequestID, NewRequest: spec},
	} {
		f := newFake13()
		f.conf = c
		res := run13(t, confirmEval(t, f))
		wantUnknown(t, name, res, "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
		wantRecordedNothing(t, name, f)
	}
}

// AGENT-16, REQ-TRU-009, INV-25: a request to make is made only from complete, well-formed values. The two values only the fresh evaluation knows (the evidence set hash and the
// policy version), the intent's own values, the TTL and the approver requirement are checked; Trust chooses none of them. Any failure is UNKNOWN and nothing is recorded.
func TestT13ANewRequestNeedsWellFormedBindingValuesAndASafeSpec(t *testing.T) {
	for name, mutate := range map[string]func(*evaluation, *f13){
		"no evidence set hash":                   func(e *evaluation, _ *f13) { e.evidenceSetHash = "" },
		"an evidence set hash of the wrong form": func(e *evaluation, _ *f13) { e.evidenceSetHash = "sha256:ABC" },
		"no policy version (none took part)":     func(e *evaluation, _ *f13) { e.policyVersions = nil },
		"no policy version (two policies)": func(e *evaluation, _ *f13) {
			e.policyVersions = []decide.PolicyVersionRef{{PolicyID: "pol_synth01", Version: "3"}, {PolicyID: "pol_synth02", Version: "1"}}
		},
		"a policy version with a character outside the binding's set": func(e *evaluation, _ *f13) {
			e.policyVersions = []decide.PolicyVersionRef{{PolicyID: "pol synth01", Version: "3"}}
		},
		"no principal":                          func(e *evaluation, _ *f13) { e.intent.PrincipalID = "" },
		"an intent hash of the wrong form":      func(e *evaluation, _ *f13) { e.intent.IntentHash = "sha256:ABC" },
		"a zero TTL":                            func(_ *evaluation, f *f13) { f.conf.NewRequest.TTL = 0 },
		"a negative TTL":                        func(_ *evaluation, f *f13) { f.conf.NewRequest.TTL = -time.Minute },
		"a TTL under one second":                func(_ *evaluation, f *f13) { f.conf.NewRequest.TTL = 500 * time.Millisecond },
		"no requirement":                        func(_ *evaluation, f *f13) { f.conf.NewRequest.Requirement = "" },
		"a requirement that is not in the enum": func(_ *evaluation, f *f13) { f.conf.NewRequest.Requirement = "OWNER" },
		"a lower-case requirement":              func(_ *evaluation, f *f13) { f.conf.NewRequest.Requirement = "self" },
		"a role with no name":                   func(_ *evaluation, f *f13) { f.conf.NewRequest.Requirement = "ROLE:" },
		"a role in upper case":                  func(_ *evaluation, f *f13) { f.conf.NewRequest.Requirement = "ROLE:SUPERVISOR" },
	} {
		f := pendingNew(newFake13())
		ev := confirmEval(t, f)
		mutate(ev, f)
		res := run13(t, ev)
		wantUnknown(t, name, res, "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
		wantRecordedNothing(t, name, f)
	}
}

// A plan step whose target or capability holds a character the binding cannot carry is not bindable: UNKNOWN, nothing recorded (the Recorder would refuse it; the evaluation ends first).
func TestT13AStepWithAnUnbindableValueIsUnknownAndNothingIsRecorded(t *testing.T) {
	f := pendingNew(newFake13())
	f.plan = Plan{Hash: tPlanHash, Steps: []PlanStep{{CapabilityID: "fin.cap.create_escalation_case", TargetRef: "txn synth", ParamsHash: tHash}}}
	ev := confirmEval(t, f)
	ev.intent.TargetRef = "txn synth"
	wantUnknown(t, "a target with a space", run13(t, ev), "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
	wantRecordedNothing(t, "a target with a space", f)
}

// The requirements the contract lists are accepted as they are: SELF, MAKER_CHECKER and ROLE:<role>.
func TestT13ANewRequestKeepsTheRequirementOfTheSpec(t *testing.T) {
	for _, req := range []string{"SELF", "MAKER_CHECKER", "ROLE:supervisor", "ROLE:l2_support"} {
		f := pendingNew(newFake13())
		f.conf.NewRequest.Requirement = req
		res := run13(t, confirmEval(t, f))
		if res.Decision.Outcome() != decide.Confirm {
			t.Errorf("%s: outcome %s %v, want CONFIRM", req, res.Decision.Outcome(), codes13(res.Decision))
			continue
		}
		if got := rec0(t, f).ConfirmationRequest; got == nil || got.Requirement != req || got.RequesterID != t13Principal {
			t.Errorf("%s: the request recorded is %+v", req, got)
		}
	}
}

// REQ-TRU-009, INV-25, REQ-PLN-002: a confirmation approves ONE action, so the plan T13 asks for to bind a new request has exactly one write step and passes every check T14 makes.
// Anything else ends the evaluation in UNKNOWN with the result T14 gives (PLAN_INVALID, or INTERNAL_ERROR for a dependency that failed); nothing is recorded.
func TestT13ANewRequestNeedsAValidPlanOfExactlyOneWriteStep(t *testing.T) {
	one := PlanStep{CapabilityID: "fin.cap.create_escalation_case", TargetRef: "txn_synth0001", ParamsHash: tHash}
	other := PlanStep{CapabilityID: "fin.cap.route_to_internal_owner", TargetRef: "txn_synth0001", ParamsHash: tHash}
	for _, c := range []struct {
		name    string
		plan    Plan
		planErr error
		write   bool
		capErr  error
		code    string
		failed  bool
	}{
		{name: "the planner says the plan is invalid", planErr: ErrPlanInvalid, write: true, code: "PLAN_INVALID"},
		{name: "a plan with no hash", plan: Plan{Steps: []PlanStep{one}}, write: true, code: "PLAN_INVALID"},
		{name: "a write plan with no step", plan: Plan{Hash: tPlanHash}, write: true, code: "PLAN_INVALID"},
		{name: "a plan with two steps", plan: Plan{Hash: tPlanHash, Steps: []PlanStep{one, other}}, write: true, code: "PLAN_INVALID"},
		{name: "a read, which has no step to bind", plan: Plan{Hash: tPlanHash}, write: false, code: "PLAN_INVALID"},
		{name: "a step on another target than the intent's", plan: Plan{Hash: tPlanHash, Steps: []PlanStep{{CapabilityID: one.CapabilityID, TargetRef: "txn_other0001", ParamsHash: tHash}}}, write: true, code: "PLAN_INVALID"},
		{name: "a step on a capability the registry does not know", plan: Plan{Hash: tPlanHash, Steps: []PlanStep{{CapabilityID: "fin.cap.unknown", TargetRef: "txn_synth0001", ParamsHash: tHash}}}, write: true, code: "PLAN_INVALID"},
		{name: "the planner fails", planErr: errInjected, write: true, code: "INTERNAL_ERROR", failed: true},
		{name: "the registry fails", plan: Plan{Hash: tPlanHash, Steps: []PlanStep{one}}, write: true, capErr: errInjected, code: "INTERNAL_ERROR", failed: true},
	} {
		f := pendingNew(newFake13())
		f.plan, f.planErr, f.capErr = c.plan, c.planErr, c.capErr
		ev := confirmEval(t, f)
		ev.late.needsWrite = c.write
		res := run13(t, ev)
		tr := decide.StepTerminal
		if c.failed {
			tr = decide.StepFailed
		}
		wantUnknown(t, c.name, res, c.code, decide.T13Confirmation, tr)
		wantRecordedNothing(t, c.name, f)
	}
}

// EC-E16 (record first): the CONFIRM stands only if the Recorder wrote it. A Recorder that fails, one that answers with something that is not a decision ID, one that names a grant
// for a CONFIRM, and (for a request to make) one that names no request or a malformed one: UNKNOWN, never CONFIRM, with no ID returned.
func TestT13AConfirmIsReturnedOnlyWhenTheRecorderWroteIt(t *testing.T) {
	type tc struct {
		name   string
		start  func(*f13) *f13
		rec    RecordResult
		recErr error
	}
	for _, c := range []tc{
		{"a Recorder error that comes with a full answer (a request to make)", pendingNew, RecordResult{DecisionID: "dec_synth0002", ConfirmationRequestID: "cfr_synth0002"}, errInjected},
		{"a Recorder error that comes with a full answer (an open request)", pendingOpen, RecordResult{DecisionID: "dec_synth0002"}, errInjected},
		{"a Recorder error with no answer", pendingOpen, RecordResult{}, errInjected},
		{"no decision ID (a request to make)", pendingNew, RecordResult{ConfirmationRequestID: "cfr_synth0002"}, nil},
		{"no decision ID (an open request)", pendingOpen, RecordResult{}, nil},
		{"a decision ID of another kind", pendingOpen, RecordResult{DecisionID: "grt_synth0001"}, nil},
		{"a grant for a CONFIRM (a request to make)", pendingNew, RecordResult{DecisionID: "dec_synth0002", ConfirmationRequestID: "cfr_synth0002", GrantIDs: []string{"grt_synth0001"}}, nil},
		{"a grant for a CONFIRM (an open request)", pendingOpen, RecordResult{DecisionID: "dec_synth0002", GrantIDs: []string{"grt_synth0001"}}, nil},
		{"a request ID for an open request, which asked for none", pendingOpen, RecordResult{DecisionID: "dec_synth0002", ConfirmationRequestID: "cfr_synth0002"}, nil},
		{"no request ID for a request to make", pendingNew, RecordResult{DecisionID: "dec_synth0002"}, nil},
		{"a request ID of another kind for a request to make", pendingNew, RecordResult{DecisionID: "dec_synth0002", ConfirmationRequestID: "cfe_synth0002"}, nil},
	} {
		f := c.start(newFake13())
		f.rec, f.recErr = c.rec, c.recErr
		res := run13(t, confirmEval(t, f))
		wantUnknown(t, c.name, res, "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
		if res.ConfirmationRequestID != "" {
			t.Errorf("%s: a request ID %q was returned with a decision that is not a CONFIRM", c.name, res.ConfirmationRequestID)
		}
		if f.called("Recorder.Record") != 1 {
			t.Errorf("%s: Record was called %d times, want once", c.name, f.called("Recorder.Record"))
		}
	}
}

// A CONFIRM cannot be built without a basis or for an intent that is not resolved; in both cases nothing is recorded (the decision is built before the Recorder is called).
func TestT13NothingIsRecordedWhenTheConfirmCannotBeBuilt(t *testing.T) {
	for name, prep := range map[string]func(*testing.T, *evaluation){
		"no policy recorded": func(t *testing.T, e *evaluation) { e.bb = freshBuilder(t, true, false) },
		"no builder":         func(_ *testing.T, e *evaluation) { e.bb = nil },
		"an unresolved intent": func(_ *testing.T, e *evaluation) {
			e.intent.View = decide.IntentView{Version: t13Version, MaterialFlags: []string{"AMBIGUOUS_COUNTERPARTY"}}
		},
	} {
		for _, start := range []func(*f13) *f13{pendingNew, pendingOpen} {
			f := start(newFake13())
			ev := confirmEval(t, f)
			prep(t, ev)
			wantUnknown(t, name, run13(t, ev), "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
			wantRecordedNothing(t, name, f)
		}
	}
}

// The Given path is unchanged: it continues, plans at T14, records the ALLOW with the confirmation's event and plan hash, and the Result carries no request ID.
func TestT13GivenStillContinuesAndRecordsTheAllowWithTheConfirmationEvent(t *testing.T) {
	f := newFake13()
	f.conf = given13()
	res := run13(t, confirmEval(t, f))
	if res.Decision.Outcome() != decide.Allow {
		t.Fatalf("outcome %s %v, want ALLOW", res.Decision.Outcome(), codes13(res.Decision))
	}
	r := rec0(t, f)
	if f.called("Recorder.Record") != 1 || r.ConfirmationEventID != "cfe_synth0001" || r.ConfirmationPlanHash != tPlanHash || r.ConfirmationRequest != nil || len(r.Steps) != 1 {
		t.Errorf("calls %v; the ALLOW record is %+v", f.calls, r)
	}
	if res.ConfirmationRequestID != "" || len(res.GrantIDs) != 1 {
		t.Errorf("request %q, grants %v", res.ConfirmationRequestID, res.GrantIDs)
	}
}

// A request is asked of the Recorder for a CONFIRM only: an ALLOW (T15) never carries one.
func TestT15NeverCarriesAConfirmationRequest(t *testing.T) {
	f := newFake13()
	res := run13(t, confirmEval(t, f))
	if res.Decision.Outcome() != decide.Allow || rec0(t, f).ConfirmationRequest != nil {
		t.Errorf("outcome %s, request %+v", res.Decision.Outcome(), rec0(t, f).ConfirmationRequest)
	}
}
