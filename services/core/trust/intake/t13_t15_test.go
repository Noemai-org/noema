package intake

// TRU-07: T13 (confirmation), T14 (plan) and T15 (the decision recorded, then the grants) of PERD 23.3 (REQ-TRU-002, REQ-TRU-008, REQ-TRU-009, REQ-PLN-002, REQ-NFR-002,
// INV-16, INV-21, INV-25, EC-E16). The tests build the evaluation directly: T1 to T12 are replaced by steps that continue, and T13 to T15 are the real steps over
// fakes of the three ports. A fake is a test double of a port, never of a failure mode of the system under test.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

const (
	tTenant    = "ten_synth01"
	tPrincipal = "usr_synth01"
	tIntent    = "int_synth0001"
	tVersion   = 2
	tHash      = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tPlanHash  = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

var errInjected = errors.New("injected fault")

type fake struct {
	calls   []string
	conf    Confirmation
	confErr error
	plan    Plan
	planErr error
	planReq []PlanRequest
	rec     RecordResult
	recErr  error
	recReqs []RecordRequest
	failAt  string
	panicAt string
	hasDL   []bool // whether the context of each call had a deadline
}

func (f *fake) hit(ctx context.Context, name string) error {
	f.calls = append(f.calls, name)
	_, ok := ctx.Deadline()
	f.hasDL = append(f.hasDL, ok)
	if f.panicAt == name {
		panic("injected panic in " + name)
	}
	if f.failAt == name {
		return errInjected
	}
	return nil
}

func (f *fake) Status(ctx context.Context, _ AdmittedIntent) (Confirmation, error) {
	return f.conf, firstErr(f.hit(ctx, "Confirmations.Status"), f.confErr)
}

func (f *fake) Plan(ctx context.Context, r PlanRequest) (Plan, error) {
	f.planReq = append(f.planReq, r)
	return f.plan, firstErr(f.hit(ctx, "Planner.Plan"), f.planErr)
}

func (f *fake) Record(ctx context.Context, r RecordRequest) (RecordResult, error) {
	f.recReqs = append(f.recReqs, r)
	return f.rec, firstErr(f.hit(ctx, "Recorder.Record"), f.recErr)
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func rec0(t *testing.T, f *fake) RecordRequest {
	t.Helper()
	if len(f.recReqs) == 0 {
		t.Fatalf("Record was never called; calls %v", f.calls)
	}
	return f.recReqs[0]
}

func (f *fake) called(name string) int {
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

// newFake answers like a write request that needs no confirmation, with a one-step plan and a Recorder that succeeds.
func newFake() *fake {
	return &fake{
		conf: Confirmation{Status: ConfirmationNotRequired},
		plan: Plan{Hash: tPlanHash, Steps: []PlanStep{{CapabilityID: "fin.cap.create_escalation_case", TargetRef: "txn_synth0001", ParamsHash: tHash}}},
		rec:  RecordResult{DecisionID: "dec_synth0001", GrantIDs: []string{"grt_synth0001"}},
	}
}

func freshBuilder(t *testing.T, evidence, policy bool) *decide.BasisBuilder {
	t.Helper()
	bb, err := decide.NewBasisBuilder("1.0.0", time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC).Unix()) // SYNTHETIC clock
	if err != nil {
		t.Fatal(err)
	}
	if evidence {
		fresh := []decide.Freshness{{EvidenceID: "evd_synth01", EffectiveAt: 1, Source: "fin.source.ledger", AgeSeconds: 1, EffectiveLimitSeconds: 60, LimitSetBy: decide.LimitBySource}}
		if err := bb.RecordEvidence("evs_synth01", fresh); err != nil {
			t.Fatal(err)
		}
	}
	if policy {
		if err := bb.RecordPolicy([]decide.PolicyRuleVersion{{RuleID: "rule_synth01", PolicyID: "pol_synth01", Version: "1"}}); err != nil {
			t.Fatal(err)
		}
	}
	return bb
}

func newEval(t *testing.T, f *fake) *evaluation {
	t.Helper()
	view := decide.IntentView{Version: tVersion, RequiredSlots: map[string]decide.SlotStatus{"subject": decide.SlotResolved}}
	a := AdmittedIntent{TenantID: tTenant, IntentID: tIntent, Version: tVersion, IntentNode: "fin.payments.exception.investigate", IntentHash: tHash,
		CorrelationID: "corr_synth0001", PrincipalID: tPrincipal, View: view}
	svc := &Service{Ports: Ports{Confirmations: f, Planner: f, Recorder: f}, Clock: clock.NewManual(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)), PackVersion: "1.0.0"}
	ev := &evaluation{svc: svc, req: Request{Session: Session{TenantID: tTenant, PrincipalID: tPrincipal, Role: "customer"}, CorrelationID: "corr_synth0001", IntentRef: tIntent + "@2"},
		intentID: tIntent, version: tVersion, intent: &a, basis: freshBuilder(t, true, true)}
	ev.late.needsWrite = true
	return ev
}

// lateSteps is T1 to T12 as steps that continue, and T13 to T15 as the real steps.
func (e *evaluation) lateSteps() decide.Steps {
	var s decide.Steps
	for i := range s {
		s[i] = func(context.Context) decide.StepResult { return decide.Continue() }
	}
	s[decide.T13Confirmation-1], s[decide.T14Plan-1], s[decide.T15DecisionRecorded-1] = e.t13, e.t14, e.t15
	return s
}

func tctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func run(t *testing.T, ev *evaluation) Result {
	t.Helper()
	return ev.run(tctx(t), ev.lateSteps())
}

func codes(d decide.Decision) []string {
	out := []string{}
	for _, r := range d.Reasons() {
		out = append(out, r.String())
	}
	return out
}

func lastTrace(r Result) decide.TraceEntry { return r.Trace[len(r.Trace)-1] }

func stepResult(r Result, s decide.GateStep) (decide.TraceResult, bool) {
	for _, e := range r.Trace {
		if e.Step == s {
			return e.Result, true
		}
	}
	return "", false
}

// wantUnknown asserts UNKNOWN with the one core reason and that the evaluation ended at the step with the given trace result.
func wantUnknown(t *testing.T, name string, r Result, reason string, step decide.GateStep, tr decide.TraceResult) {
	t.Helper()
	if r.Decision.Outcome() == decide.Allow {
		t.Fatalf("%s: ALLOW", name)
	}
	if r.Decision.Outcome() != decide.Unknown || !reflect.DeepEqual(codes(r.Decision), []string{reason}) {
		t.Errorf("%s: %s %v, want UNKNOWN [%s]", name, r.Decision.Outcome(), codes(r.Decision), reason)
	}
	if got := lastTrace(r); got.Step != step || got.Result != tr {
		t.Errorf("%s: trace ends %v, want %v %s", name, got, step, tr)
	}
	if r.DecisionID != "" || len(r.GrantIDs) != 0 {
		t.Errorf("%s: IDs returned with a decision that is not an ALLOW: %q %v", name, r.DecisionID, r.GrantIDs)
	}
}

// ---- T13 ----

// REQ-TRU-009: a confirmation the profile requires and that is not resolved stops at T13 with CONFIRM, which rests on the evaluated set and all three layers at PASS;
// nothing is planned and nothing is recorded.
func TestT13ConfirmationRequiredAndMissingStopsWithConfirm(t *testing.T) {
	f := newFake()
	f.conf = Confirmation{Status: ConfirmationPending}
	ev := newEval(t, f)
	res := run(t, ev)
	d := res.Decision
	if d.Outcome() != decide.Confirm {
		t.Fatalf("outcome %s %v, want CONFIRM", d.Outcome(), codes(d))
	}
	if got := lastTrace(res); got.Step != decide.T13Confirmation || got.Result != decide.StepTerminal {
		t.Errorf("trace ends %v, want T13 terminal", got)
	}
	if !d.Authority().AllPass() {
		t.Errorf("authority %v, want all PASS", d.Authority())
	}
	if err := ev.basis.Check(d); err != nil {
		t.Errorf("the CONFIRM's basis is not the evaluated set: %v", err)
	}
	if f.called("Planner.Plan") != 0 || f.called("Recorder.Record") != 0 {
		t.Errorf("the planner or the recorder was called after a CONFIRM: %v", f.calls)
	}
}

func TestT13ConfirmationNotRequiredContinues(t *testing.T) {
	f := newFake()
	res := run(t, newEval(t, f))
	if tr, _ := stepResult(res, decide.T13Confirmation); tr != decide.StepPassed {
		t.Fatalf("T13 %v, want PASSED; trace %v", tr, res.Trace)
	}
	if res.Decision.Outcome() != decide.Allow {
		t.Errorf("outcome %s %v, want ALLOW from the happy path", res.Decision.Outcome(), codes(res.Decision))
	}
	if got := rec0(t, f).ConfirmationEventID; got != "" {
		t.Errorf("a confirmation event %q was named although none was required", got)
	}
}

// INV-25: a resolved confirmation leads to the fresh evaluation this is; the grant follows the confirmation event.
func TestT13ConfirmationGivenContinuesAndTheGrantFollowsItsEvent(t *testing.T) {
	f := newFake()
	f.conf = Confirmation{Status: ConfirmationGiven, EventID: "evt_synth0001"}
	res := run(t, newEval(t, f))
	if tr, _ := stepResult(res, decide.T13Confirmation); tr != decide.StepPassed {
		t.Fatalf("T13 %v, want PASSED; trace %v", tr, res.Trace)
	}
	if res.Decision.Outcome() != decide.Allow || rec0(t, f).ConfirmationEventID != "evt_synth0001" {
		t.Errorf("outcome %s, confirmation event %q", res.Decision.Outcome(), rec0(t, f).ConfirmationEventID)
	}
}

// A status that is not one of the three, and a Given with no event, are not answers: UNKNOWN, never a continue.
func TestT13AnswerThatIsNotAnAnswerIsUnknown(t *testing.T) {
	for _, c := range []struct {
		name string
		conf Confirmation
	}{
		{"the zero status", Confirmation{}},
		{"a status outside the three", Confirmation{Status: ConfirmationGiven + 1, EventID: "evt_synth0001"}},
		{"given with no event", Confirmation{Status: ConfirmationGiven}},
	} {
		f := newFake()
		f.conf = c.conf
		res := run(t, newEval(t, f))
		wantUnknown(t, c.name, res, "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
		if f.called("Recorder.Record") != 0 {
			t.Errorf("%s: Record was called", c.name)
		}
	}
}

// A CONFIRM rests on the evaluated set; without the evidence and the policy there is nothing to rest on (INV-16), and an intent that is not resolved cannot be confirmed.
func TestT13CannotConfirmWithoutABasisOrWithAnUnresolvedIntent(t *testing.T) {
	f := newFake()
	f.conf = Confirmation{Status: ConfirmationPending}
	ev := newEval(t, f)
	ev.basis = freshBuilder(t, true, false)
	wantUnknown(t, "no policy recorded", run(t, ev), "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)

	ev = newEval(t, f)
	ev.basis = nil
	wantUnknown(t, "no builder", run(t, ev), "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)

	ev = newEval(t, f)
	ev.intent.View = decide.IntentView{Version: tVersion, MaterialFlags: []string{"AMBIGUOUS_COUNTERPARTY"}}
	wantUnknown(t, "unresolved intent", run(t, ev), "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)

	ev = newEval(t, f)
	ev.intent = nil
	wantUnknown(t, "no intent", run(t, ev), "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
}

// ---- T14 ----

// REQ-PLN-002: Trust asks for a plan with what it learned, and the steps and the hash it gets back are what T15 records.
func TestT14AsksForThePlanAndCarriesItsStepsToTheRecorder(t *testing.T) {
	f := newFake()
	res := run(t, newEval(t, f))
	if res.Decision.Outcome() != decide.Allow {
		t.Fatalf("outcome %s %v", res.Decision.Outcome(), codes(res.Decision))
	}
	want := PlanRequest{TenantID: tTenant, CorrelationID: "corr_synth0001", IntentID: tIntent, IntentVersion: tVersion, IntentHash: tHash, IntentNode: "fin.payments.exception.investigate", WriteRequired: true}
	if len(f.planReq) != 1 || f.planReq[0] != want {
		t.Errorf("plan request %+v, want %+v", f.planReq, want)
	}
	r := rec0(t, f)
	if r.PlanHash != tPlanHash || !reflect.DeepEqual(r.Steps, []RecordStep{{PlanStep: 0, CapabilityID: "fin.cap.create_escalation_case", TargetRef: "txn_synth0001", ParamsHash: tHash}}) {
		t.Errorf("record request plan %q %+v", r.PlanHash, r.Steps)
	}
}

// T14's stop set is empty (23.3): an invalid plan is UNKNOWN PLAN_INVALID, a defect alert, and nothing is recorded.
func TestT14AnInvalidPlanIsUnknownPlanInvalid(t *testing.T) {
	step := PlanStep{CapabilityID: "fin.cap.create_escalation_case", TargetRef: "txn_synth0001", ParamsHash: tHash}
	for _, c := range []struct {
		name     string
		plan     Plan
		planErr  error
		write    bool
		wantCode string
	}{
		{"the planner says the plan is invalid", Plan{}, ErrPlanInvalid, true, "PLAN_INVALID"},
		{"the planner says so, wrapped", Plan{}, errors.Join(errors.New("registry"), ErrPlanInvalid), true, "PLAN_INVALID"},
		{"no hash", Plan{Steps: []PlanStep{step}}, nil, true, "PLAN_INVALID"},
		{"a hash of the wrong form", Plan{Hash: "sha256:ABC", Steps: []PlanStep{step}}, nil, true, "PLAN_INVALID"},
		{"a write with no step", Plan{Hash: tPlanHash}, nil, true, "PLAN_INVALID"},
		{"a read with a write step", Plan{Hash: tPlanHash, Steps: []PlanStep{step}}, nil, false, "PLAN_INVALID"},
		{"a read with no hash", Plan{}, nil, false, "PLAN_INVALID"},
		{"a step with no capability", Plan{Hash: tPlanHash, Steps: []PlanStep{{TargetRef: "txn_synth0001", ParamsHash: tHash}}}, nil, true, "PLAN_INVALID"},
		{"a step with no target", Plan{Hash: tPlanHash, Steps: []PlanStep{{CapabilityID: "fin.cap.create_escalation_case", ParamsHash: tHash}}}, nil, true, "PLAN_INVALID"},
		{"a step with a params hash of the wrong form", Plan{Hash: tPlanHash, Steps: []PlanStep{{CapabilityID: "fin.cap.create_escalation_case", TargetRef: "txn_synth0001", ParamsHash: "x"}}}, nil, true, "PLAN_INVALID"},
		{"one good step and one bad", Plan{Hash: tPlanHash, Steps: []PlanStep{step, {}}}, nil, true, "PLAN_INVALID"},
	} {
		f := newFake()
		f.plan, f.planErr = c.plan, c.planErr
		ev := newEval(t, f)
		ev.late.needsWrite = c.write
		res := run(t, ev)
		wantUnknown(t, c.name, res, c.wantCode, decide.T14Plan, decide.StepTerminal)
		if f.called("Recorder.Record") != 0 {
			t.Errorf("%s: Record was called for an invalid plan", c.name)
		}
	}
}

// A6/A205 Q3: for a read the plan is empty and has its hash, so the Decision Record always names a plan; the ALLOW issues no grant.
func TestT14AReadHasAnEmptyPlanWithItsHashAndGetsNoGrant(t *testing.T) {
	f := newFake()
	f.plan = Plan{Hash: tPlanHash}
	f.rec = RecordResult{DecisionID: "dec_synth0002"}
	ev := newEval(t, f)
	ev.late.needsWrite = false
	res := run(t, ev)
	if res.Decision.Outcome() != decide.Allow || res.DecisionID != "dec_synth0002" || len(res.GrantIDs) != 0 {
		t.Fatalf("outcome %s %v, id %q, grants %v", res.Decision.Outcome(), codes(res.Decision), res.DecisionID, res.GrantIDs)
	}
	if len(f.planReq) == 1 && f.planReq[0].WriteRequired {
		t.Error("the plan request said a write is required for a read")
	}
	if r := rec0(t, f); r.PlanHash != tPlanHash || len(r.Steps) != 0 {
		t.Errorf("the record request for a read: plan %q, %d steps", r.PlanHash, len(r.Steps))
	}
}

// ---- T15 ----

// EC-E16: the ALLOW is returned only after the Recorder has written, once, with the decision the evaluated set stands behind; its IDs come from what was written.
func TestT15AllowStandsOnlyAfterTheRecordWasWrittenOnce(t *testing.T) {
	f := newFake()
	ev := newEval(t, f)
	res := run(t, ev)
	if res.Decision.Outcome() != decide.Allow {
		t.Fatalf("outcome %s %v", res.Decision.Outcome(), codes(res.Decision))
	}
	if f.called("Recorder.Record") != 1 {
		t.Fatalf("Record called %d times, want once; calls %v", f.called("Recorder.Record"), f.calls)
	}
	if f.calls[len(f.calls)-1] != "Recorder.Record" {
		t.Errorf("Record is not the last port call: %v", f.calls)
	}
	if got := lastTrace(res); got.Step != decide.T15DecisionRecorded || got.Result != decide.StepTerminal {
		t.Errorf("trace ends %v, want T15 terminal", got)
	}
	if res.DecisionID != "dec_synth0001" || !reflect.DeepEqual(res.GrantIDs, []string{"grt_synth0001"}) {
		t.Errorf("IDs %q %v, want those the Recorder returned", res.DecisionID, res.GrantIDs)
	}
	if !res.Decision.Authority().AllPass() {
		t.Errorf("authority %v, want all PASS", res.Decision.Authority())
	}
	for i, dl := range f.hasDL {
		if !dl {
			t.Errorf("port call %d had a context with no deadline", i)
		}
	}
}

// TRU-13: what T15 hands to the Recorder is the decision carrying the builder's basis, the builder itself and its digest, with the identifiers of the request.
func TestT15TheRecordRequestCarriesTheBuildersBasisAndTheRequestsIdentity(t *testing.T) {
	f := newFake()
	f.conf = Confirmation{Status: ConfirmationGiven, EventID: "evt_synth0001"}
	ev := newEval(t, f)
	run(t, ev)
	r := rec0(t, f)
	digest, err := ev.basis.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if r.Evaluated != ev.basis || r.BasisDigest != digest || ev.basis.Check(r.Decision) != nil {
		t.Errorf("the request does not carry the evaluated set: digest %q want %q", r.BasisDigest, digest)
	}
	if r.Decision.Outcome() != decide.Allow || !r.Decision.HasCompleteBasis() {
		t.Errorf("the decision in the request: %s, complete basis %v", r.Decision.Outcome(), r.Decision.HasCompleteBasis())
	}
	if r.TenantID != tTenant || r.CorrelationID != "corr_synth0001" || r.PrincipalID != tPrincipal || r.IntentID != tIntent || r.IntentHash != tHash || r.IntentVersion != tVersion {
		t.Errorf("identity in the request: %+v", r)
	}
}

// A mistaken or forged basis cannot stand: the ALLOW is checked against the builder of the call (EvaluateBound), so a decision whose basis is another evaluation's is UNKNOWN.
func TestT15ABasisThatIsNotTheEvaluatedSetIsUnknown(t *testing.T) {
	f := newFake()
	ev := newEval(t, f)
	// A builder that differs from the one the steps attach from: the same shape, another pack version.
	otherBB, err := decide.NewBasisBuilder("2.0.0", time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC).Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := otherBB.RecordEvidence("evs_other01", []decide.Freshness{{EvidenceID: "evd_other01", EffectiveAt: 1, Source: "fin.source.ledger", AgeSeconds: 1, EffectiveLimitSeconds: 60, LimitSetBy: decide.LimitBySource}}); err != nil {
		t.Fatal(err)
	}
	if err := otherBB.RecordPolicy([]decide.PolicyRuleVersion{{RuleID: "rule_other01", PolicyID: "pol_other01", Version: "9"}}); err != nil {
		t.Fatal(err)
	}
	d, trace := decide.EvaluateBound(tctx(t), ev.version, ev.lateSteps(), otherBB)
	res := Result{Decision: d, Trace: trace}
	wantUnknown(t, "a basis that is not the evaluated set", res, "INTERNAL_ERROR", decide.T15DecisionRecorded, decide.StepInvalidResult)
}

// REQ-NFR-002: a Record error never gives ALLOW and a panic in Record never gives ALLOW; nothing of the decision is returned.
func TestT15ARecordErrorOrPanicIsUnknownNeverAllow(t *testing.T) {
	f := newFake()
	f.failAt = "Recorder.Record"
	wantUnknown(t, "Record error", run(t, newEval(t, f)), "INTERNAL_ERROR", decide.T15DecisionRecorded, decide.StepFailed)

	f = newFake()
	f.panicAt = "Recorder.Record"
	wantUnknown(t, "Record panic", run(t, newEval(t, f)), "INTERNAL_ERROR", decide.T15DecisionRecorded, decide.StepPanicked)

	f = newFake()
	f.recErr = ErrNotFound
	wantUnknown(t, "Record error from the adapter", run(t, newEval(t, f)), "INTERNAL_ERROR", decide.T15DecisionRecorded, decide.StepFailed)
}

// A Record that answers without error but not with what a write returns (no decision ID, a malformed one, or the wrong number of grants) is not trusted.
func TestT15AResultThatIsNotWhatAWriteReturnsIsUnknown(t *testing.T) {
	for _, c := range []struct {
		name string
		rec  RecordResult
	}{
		{"no decision ID", RecordResult{GrantIDs: []string{"grt_synth0001"}}},
		{"a decision ID of the wrong form", RecordResult{DecisionID: "x", GrantIDs: []string{"grt_synth0001"}}},
		{"no grant for a write step", RecordResult{DecisionID: "dec_synth0001"}},
		{"a grant too many", RecordResult{DecisionID: "dec_synth0001", GrantIDs: []string{"grt_synth0001", "grt_synth0002"}}},
		{"a grant ID of the wrong form", RecordResult{DecisionID: "dec_synth0001", GrantIDs: []string{"y"}}},
	} {
		f := newFake()
		f.rec = c.rec
		wantUnknown(t, c.name, run(t, newEval(t, f)), "INTERNAL_ERROR", decide.T15DecisionRecorded, decide.StepFailed)
	}
}

// EC-E16 and the order of the steps: Record is called only for an ALLOW candidate, that is, when T1 to T14 passed. A stop at any earlier step leaves it uncalled.
func TestT15RecordIsNotCalledWhenAnEarlierStepStopped(t *testing.T) {
	stopAt := func(ev *evaluation, step decide.GateStep, d decide.Decision) decide.Steps {
		s := ev.lateSteps()
		s[step-1] = func(context.Context) decide.StepResult { return decide.Stop(d) }
		return s
	}
	r, _ := decide.Admit(newEval(t, newFake()).intent.View).Resolved()
	reject, err := decide.FailAtGate(decide.T5Scope, tVersion)
	if err != nil {
		t.Fatal(err)
	}
	escalate, err := r.Escalate(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}, mustCore(t, "INSUFFICIENT_EVIDENCE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		step decide.GateStep
		d    decide.Decision
	}{{decide.T5Scope, reject}, {decide.T9Evidence, escalate}, {decide.T12Clocks, escalate}} {
		f := newFake()
		ev := newEval(t, f)
		res := ev.run(tctx(t), stopAt(ev, c.step, c.d))
		if lastTrace(res).Step != c.step {
			t.Fatalf("stop at %v: trace %v", c.step, res.Trace)
		}
		if len(f.calls) != 0 {
			t.Errorf("stop at %v: ports called: %v", c.step, f.calls)
		}
	}
	// A failed step before T13 likewise.
	f := newFake()
	ev := newEval(t, f)
	s := ev.lateSteps()
	s[decide.T10Reconciliation-1] = func(context.Context) decide.StepResult { return decide.FailStep(errInjected) }
	if res := ev.run(tctx(t), s); res.Decision.Outcome() != decide.Unknown || len(f.calls) != 0 {
		t.Errorf("a failed T10: %s, calls %v", res.Decision.Outcome(), f.calls)
	}
	// CONFIRM at T13 and an invalid plan at T14 leave it uncalled too (covered above); a missing basis at T15 as well, before the call.
	f = newFake()
	ev = newEval(t, f)
	ev.basis = freshBuilder(t, false, true)
	wantUnknown(t, "no evidence recorded", run(t, ev), "INTERNAL_ERROR", decide.T15DecisionRecorded, decide.StepFailed)
	if f.called("Recorder.Record") != 0 {
		t.Error("Record was called with an incomplete basis")
	}
}

func mustCore(t *testing.T, code string) decide.ReasonCode {
	t.Helper()
	r, err := decide.Core(code)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// An unresolved intent never reaches an ALLOW (INV-06): T15 refuses it and Record is not called.
func TestT15AnUnresolvedIntentIsNeverRecordedAsAllow(t *testing.T) {
	f := newFake()
	ev := newEval(t, f)
	ev.intent.View = decide.IntentView{Version: tVersion, RequiredSlots: map[string]decide.SlotStatus{"subject": decide.SlotAmbiguous}}
	wantUnknown(t, "unresolved", run(t, ev), "INTERNAL_ERROR", decide.T15DecisionRecorded, decide.StepFailed)
	if f.called("Recorder.Record") != 0 {
		t.Error("Record was called for an unresolved intent")
	}
}

// ---- every port: an error and a panic are UNKNOWN, never ALLOW ----

func TestAnErrorOrAPanicAtAnyT13ToT15PortIsUnknownNeverAllow(t *testing.T) {
	for _, c := range []struct {
		method string
		step   decide.GateStep
	}{{"Confirmations.Status", decide.T13Confirmation}, {"Planner.Plan", decide.T14Plan}, {"Recorder.Record", decide.T15DecisionRecorded}} {
		for _, mode := range []string{"error", "panic"} {
			f := newFake()
			tr := decide.StepFailed
			if mode == "error" {
				f.failAt = c.method
			} else {
				f.panicAt, tr = c.method, decide.StepPanicked
			}
			wantUnknown(t, c.method+" "+mode, run(t, newEval(t, f)), "INTERNAL_ERROR", c.step, tr)
		}
	}
}

// A context that is done when the Recorder returns is not trusted either: the ALLOW does not stand (INV-16).
func TestAnExpiredContextAtRecordIsUnknown(t *testing.T) {
	f := newFake()
	ev := newEval(t, f)
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ev.svc.Ports.Recorder = cancelAfterRecord{f, cancel}
	res := ev.run(c, ev.lateSteps())
	wantUnknown(t, "deadline during Record", res, "INTERNAL_ERROR", decide.T15DecisionRecorded, decide.StepDeadline)
}

type cancelAfterRecord struct {
	*fake
	cancel context.CancelFunc
}

func (c cancelAfterRecord) Record(ctx context.Context, r RecordRequest) (RecordResult, error) {
	res, err := c.fake.Record(ctx, r)
	c.cancel()
	return res, err
}

// INV-21: the same answers give the same decision, the same trace, the same record request and the same IDs.
func TestT13ToT15AreDeterministic(t *testing.T) {
	var results []Result
	var reqs []RecordRequest
	for i := 0; i < 2; i++ {
		f := newFake()
		results = append(results, run(t, newEval(t, f)))
		reqs = append(reqs, rec0(t, f))
	}
	a, b := results[0], results[1]
	if !reflect.DeepEqual(a.Trace, b.Trace) || a.Decision.Outcome() != b.Decision.Outcome() || a.DecisionID != b.DecisionID || !reflect.DeepEqual(a.GrantIDs, b.GrantIDs) ||
		!reflect.DeepEqual(codes(a.Decision), codes(b.Decision)) || a.Decision.Basis().PackVersion != b.Decision.Basis().PackVersion {
		t.Errorf("two runs differ: %+v / %+v", a, b)
	}
	if reqs[0].BasisDigest != reqs[1].BasisDigest || reqs[0].PlanHash != reqs[1].PlanHash || !reflect.DeepEqual(reqs[0].Steps, reqs[1].Steps) {
		t.Errorf("two record requests differ: %+v / %+v", reqs[0], reqs[1])
	}
}
