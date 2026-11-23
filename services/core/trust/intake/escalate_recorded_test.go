package intake

// A534 (interim, relayed, not yet committed; REQ-TRU-010, REQ-RCV-003; INV-25, EC-E16): an ESCALATE outcome is recorded through the Recorder port, with no step and no grant, and its decision
// ID comes back in the result so that the escalation's action decisions can name it (ContinuationRequest.DispositionDecisionID, passed into the Decision Record's request). The Record comes
// first: when it fails, or answers what a decision with no step cannot be, the evaluation ends in UNKNOWN and no ESCALATE is returned. Other outcomes are recorded as before and not here.
// All data is SYNTHETIC.

import (
	"context"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// escalatedAtT12 is an evaluation whose T12 ends in ESCALATE NO_RUNNABLE_CAPABILITY, over the ports of f13; the other steps continue, T13 to T15 are real and must not be reached.
func escalatedAtT12(t *testing.T, f *f13) (*evaluation, Result) {
	t.Helper()
	ev := newEval13(t, f)
	steps := ev.lateSteps()
	steps[decide.T12Clocks-1] = func(context.Context) decide.StepResult {
		r, err := ev.resolved()
		if err != nil {
			return decide.FailStep(err)
		}
		return escalateCore(r, "NO_RUNNABLE_CAPABILITY", passed())
	}
	return ev, ev.run(tctx(t), steps)
}

// The ESCALATE is recorded: one Record call, the decision as an ESCALATE with the evaluated basis, no step, no plan, no confirmation request and no disposition; the decision ID is the result's
// and no grant comes with it; the Planner is never asked.
func TestA534AnEscalateIsRecordedThroughTheRecorderAndItsDecisionIDComesBack(t *testing.T) {
	f := newFake13()
	f.rec = RecordResult{DecisionID: "dec_synth0007"}
	_, res := escalatedAtT12(t, f)
	if res.Decision.Outcome() != decide.Escalate || !reflect.DeepEqual(codes13(res.Decision), []string{"NO_RUNNABLE_CAPABILITY"}) {
		t.Fatalf("%s %v, want ESCALATE NO_RUNNABLE_CAPABILITY", res.Decision.Outcome(), codes13(res.Decision))
	}
	if res.DecisionID != "dec_synth0007" || len(res.GrantIDs) != 0 || res.ConfirmationRequestID != "" {
		t.Errorf("result %q %v %q, want the recorded decision ID and no grant and no request", res.DecisionID, res.GrantIDs, res.ConfirmationRequestID)
	}
	if len(f.recReqs) != 1 || f.called("Planner.Plan") != 0 {
		t.Fatalf("Record called %d times, Plan %d times; want once and never", len(f.recReqs), f.called("Planner.Plan"))
	}
	r := f.recReqs[0]
	if r.Decision.Outcome() != decide.Escalate || len(r.Steps) != 0 || r.PlanHash != "" || r.ConfirmationRequest != nil || r.DispositionDecisionID != "" || r.BasisDigest == "" || r.Evaluated == nil {
		t.Errorf("the record request %+v: want an ESCALATE with its basis, no step, no plan, no request, no disposition", r)
	}
}

// The Record comes first: an error is UNKNOWN, never the ESCALATE nobody recorded; a decision ID that is not one, or a grant, is not what a decision with no step returns.
func TestA534AnEscalateThatCannotBeRecordedIsUnknown(t *testing.T) {
	cases := map[string]func(f *f13){
		"the recorder fails":      func(f *f13) { f.recErr = errInjected },
		"no decision ID":          func(f *f13) { f.rec = RecordResult{} },
		"a malformed decision ID": func(f *f13) { f.rec = RecordResult{DecisionID: "not an id"} },
		"a grant for no step":     func(f *f13) { f.rec = RecordResult{DecisionID: "dec_synth0007", GrantIDs: []string{"grt_synth0001"}} },
		"a request nobody created": func(f *f13) {
			f.rec = RecordResult{DecisionID: "dec_synth0007", ConfirmationRequestID: "cfr_synth0001"}
		},
	}
	for name, mut := range cases {
		f := newFake13()
		f.rec = RecordResult{DecisionID: "dec_synth0007"}
		mut(f)
		_, res := escalatedAtT12(t, f)
		if res.Decision.Outcome() != decide.Unknown || res.DecisionID != "" || len(res.GrantIDs) != 0 {
			t.Errorf("%s: %s %v, ID %q; want UNKNOWN and no decision ID", name, res.Decision.Outcome(), codes13(res.Decision), res.DecisionID)
		}
	}
}

// Only an ESCALATE is recorded here: a REJECT of a gate is not (its record, if any, is the caller's as before).
func TestA534ARejectIsNotRecordedByTheEscalateRule(t *testing.T) {
	f := newFake13()
	ev := newEval13(t, f)
	steps := ev.lateSteps()
	steps[decide.T5Scope-1] = func(context.Context) decide.StepResult {
		d, err := decide.FailAtGate(decide.T5Scope, ev.version)
		if err != nil {
			return decide.FailStep(err)
		}
		return decide.Stop(d)
	}
	res := ev.run(tctx(t), steps)
	if res.Decision.Outcome() != decide.Reject || len(f.recReqs) != 0 || res.DecisionID != "" {
		t.Errorf("%s, Record called %d times, ID %q; want a REJECT that is not recorded here", res.Decision.Outcome(), len(f.recReqs), res.DecisionID)
	}
}

// (2) A continuation names the disposition it follows: ContinuationRequest.DispositionDecisionID reaches the step and the Decision Record's request, and a request that names none sends none.
func TestA534AContinuationPassesItsDispositionIntoTheRecordRequest(t *testing.T) {
	if got := scopeOf(ContinuationRequest{CapabilityID: capRoute, DispositionDecisionID: "dec_synth0009"}).disposition; got != "dec_synth0009" {
		t.Errorf("the step scope names %q, want dec_synth0009", got)
	}
	for name, disposition := range map[string]string{"a disposition": "dec_synth0009", "none": ""} {
		f, p, r := stepFake()
		ev := stepEval13(t, f, p, r, capRoute)
		ev.step.disposition = disposition
		res := ev.run(tctx(t), ev.policySteps())
		if res.Decision.Outcome() != decide.Allow || len(f.recReqs) != 1 {
			t.Fatalf("%s: %s %v, Record called %d times; want ALLOW and one Record", name, res.Decision.Outcome(), codes13(res.Decision), len(f.recReqs))
		}
		if got := f.recReqs[0].DispositionDecisionID; got != disposition {
			t.Errorf("%s: the record request names %q, want %q", name, got, disposition)
		}
	}
}

// Codex and C2 on noema#903 (INV-25, EC-E16): the Record is of the decision that is RETURNED. An ESCALATE that the evaluation does not accept, because the context ended or because the step
// that returned it may not end the evaluation in ESCALATE, is UNKNOWN to the caller, and nothing is recorded for it: no Record call, hence no row and no event.
func TestA534NothingIsRecordedForAnEscalateTheEvaluationDoesNotAccept(t *testing.T) {
	// (1) a step that returns an ESCALATE it may not produce (T5 can only REJECT): the evaluation turns it into UNKNOWN
	f := newFake13()
	f.rec = RecordResult{DecisionID: "dec_synth0007"}
	ev := newEval13(t, f)
	steps := ev.lateSteps()
	steps[decide.T5Scope-1] = func(context.Context) decide.StepResult {
		r, err := ev.resolved()
		if err != nil {
			return decide.FailStep(err)
		}
		return escalateCore(r, "NO_RUNNABLE_CAPABILITY", passed())
	}
	res := ev.run(tctx(t), steps)
	if res.Decision.Outcome() != decide.Unknown || res.DecisionID != "" || len(f.recReqs) != 0 {
		t.Errorf("an ESCALATE from T5: %s, ID %q, Record called %d times; want UNKNOWN, no ID and no Record", res.Decision.Outcome(), res.DecisionID, len(f.recReqs))
	}

	// (2) the context ends while the step runs: Evaluate answers UNKNOWN whatever the step returned
	f = newFake13()
	f.rec = RecordResult{DecisionID: "dec_synth0007"}
	ev = newEval13(t, f)
	cctx, cancel := context.WithCancel(tctx(t))
	steps = ev.lateSteps()
	steps[decide.T12Clocks-1] = func(context.Context) decide.StepResult {
		r, err := ev.resolved()
		if err != nil {
			return decide.FailStep(err)
		}
		cancel()
		return escalateCore(r, "NO_RUNNABLE_CAPABILITY", passed())
	}
	res = ev.run(cctx, steps)
	if res.Decision.Outcome() != decide.Unknown || res.DecisionID != "" || len(f.recReqs) != 0 {
		t.Errorf("an ESCALATE as the context ends: %s, ID %q, Record called %d times; want UNKNOWN, no ID and no Record", res.Decision.Outcome(), res.DecisionID, len(f.recReqs))
	}
}
