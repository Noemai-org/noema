package intake

// TRU-07 (ruling relayed in the session, not yet committed: interim; REQ-TRU-009, INV-25, INV-16, EC-E16; SoT 43.2a step 4): a Declined answer is a REJECT DECLINED_BY_APPROVER at T13,
// built with the decide builders (the three authority layers PASS as at T13), on the evaluated set, RECORDED through the Recorder before it is returned. Nothing is planned, no step, no
// grant, no request. PERD 23.3 still says CONFIRM only for T13: a PERD version file and a decision record are owed (status/TRU-07-confirm-t13-rulings.md). All data is SYNTHETIC.

import (
	"context"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func declinedAnswer(f *f13) *f13 {
	f.conf = Confirmation{Status: ConfirmationDeclined, RequestID: tRequestID}
	f.rec = RecordResult{DecisionID: "dec_synth0003"}
	return f
}

// A decline: REJECT DECLINED_BY_APPROVER with all three layers PASS and the evaluated set as its basis, recorded once with no step, no grant and no request, before it is returned.
func TestT13DeclinedIsRejectDeclinedByApproverRecordedBeforeItIsReturned(t *testing.T) {
	f := declinedAnswer(newFake13())
	ev := confirmEval(t, f)
	res := run13(t, ev)
	d := res.Decision
	if d.Outcome() != decide.Reject || !reflect.DeepEqual(codes13(d), []string{"DECLINED_BY_APPROVER"}) {
		t.Fatalf("outcome %s %v, want REJECT [DECLINED_BY_APPROVER]", d.Outcome(), codes13(d))
	}
	if !d.Authority().AllPass() {
		t.Errorf("authority %v, want all three layers PASS as at T13", d.Authority())
	}
	if got := lastTrace(res); got.Step != decide.T13Confirmation || got.Result != decide.StepTerminal {
		t.Errorf("trace ends %v, want T13 terminal", got)
	}
	if !reflect.DeepEqual(f.calls, []string{"Confirmations.Status", "Recorder.Record"}) {
		t.Errorf("calls %v, want Status, then one Record: nothing is planned", f.calls)
	}
	r := rec0(t, f)
	if r.Decision.Outcome() != decide.Reject || !d.HasCompleteBasis() || r.Evaluated == nil || r.BasisDigest == "" || ev.bb.Check(r.Decision) != nil {
		t.Errorf("the REJECT recorded is %s and does not rest on the evaluated set of the builder", r.Decision.Outcome())
	}
	if len(r.Steps) != 0 || r.PlanHash != "" || r.ConfirmationEventID != "" || r.ConfirmationPlanHash != "" || r.ConfirmationRequest != nil {
		t.Errorf("the REJECT was recorded with steps %v, plan %q, confirmation %q %q or request %v: none may accompany it", r.Steps, r.PlanHash, r.ConfirmationEventID, r.ConfirmationPlanHash, r.ConfirmationRequest)
	}
	if r.TenantID != t13Tenant || r.CorrelationID != "corr_synth0001" || r.PrincipalID != t13Principal || r.IntentID != tIntent || r.IntentHash != tHash || r.IntentVersion != t13Version {
		t.Errorf("the identifiers of the record request are %+v", r)
	}
	if res.DecisionID != "dec_synth0003" || res.ConfirmationRequestID != "" || len(res.GrantIDs) != 0 {
		t.Errorf("the result names decision %q, request %q, grants %v; want dec_synth0003, none, none", res.DecisionID, res.ConfirmationRequestID, res.GrantIDs)
	}
}

// Record first (EC-E16): the REJECT is returned only if the Recorder wrote it. A Recorder that fails (also with a full answer), one that names no decision ID or one of another kind,
// one that names a grant, and one that names a request for a decision that made none: UNKNOWN, never REJECT, and no ID returned.
func TestT13ARejectForADeclineIsReturnedOnlyWhenTheRecorderWroteIt(t *testing.T) {
	for _, c := range []struct {
		name   string
		rec    RecordResult
		recErr error
	}{
		{"a Recorder error that comes with a full answer", RecordResult{DecisionID: "dec_synth0003"}, errInjected},
		{"a Recorder error with no answer", RecordResult{}, errInjected},
		{"no decision ID", RecordResult{}, nil},
		{"a decision ID of another kind", RecordResult{DecisionID: "grt_synth0001"}, nil},
		{"a grant for a REJECT", RecordResult{DecisionID: "dec_synth0003", GrantIDs: []string{"grt_synth0001"}}, nil},
		{"a request ID for a REJECT", RecordResult{DecisionID: "dec_synth0003", ConfirmationRequestID: "cfr_synth0002"}, nil},
	} {
		f := declinedAnswer(newFake13())
		f.rec, f.recErr = c.rec, c.recErr
		res := run13(t, confirmEval(t, f))
		wantUnknown(t, c.name, res, "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
		if f.called("Recorder.Record") != 1 || f.called("Planner.Plan") != 0 {
			t.Errorf("%s: calls %v, want one Record and no Planner call", c.name, f.calls)
		}
	}
}

// A Declined answer that is not one is UNKNOWN INTERNAL_ERROR and nothing is recorded or planned: no request ID, one that is not a cfr_ ID, a request to make (a decline cannot create
// one), and an event (an answer that names an event is a Given answer's shape, never a decline's).
func TestT13ADeclinedAnswerThatIsNotOneIsUnknownAndNothingIsRecorded(t *testing.T) {
	spec := &RequestSpec{Requirement: "SELF", TTL: tTTL}
	for name, c := range map[string]Confirmation{
		"no request ID":                {Status: ConfirmationDeclined},
		"a request ID of another kind": {Status: ConfirmationDeclined, RequestID: "cfe_synth0001"},
		"a malformed request ID":       {Status: ConfirmationDeclined, RequestID: "cfr_"},
		"a request to make":            {Status: ConfirmationDeclined, RequestID: tRequestID, NewRequest: spec},
		"an event":                     {Status: ConfirmationDeclined, RequestID: tRequestID, EventID: "cfe_synth0001"},
		"an event and a plan hash":     {Status: ConfirmationDeclined, RequestID: tRequestID, EventID: "cfe_synth0001", PlanHash: tPlanHash},
	} {
		f := newFake13()
		f.conf = c
		res := run13(t, confirmEval(t, f))
		wantUnknown(t, name, res, "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
		if f.called("Planner.Plan") != 0 || f.called("Recorder.Record") != 0 {
			t.Errorf("%s: the planner or the recorder was called: %v", name, f.calls)
		}
	}
}

// A decline needs no binding: it grants nothing, so empty fresh values do not stop it (they stop a Given answer only), and a plan hash left on it is not read.
func TestT13ADeclineIsRecordedEvenWhenTheFreshValuesAreEmptyAndItsPlanHashIsNotRead(t *testing.T) {
	f := declinedAnswer(newFake13())
	f.conf.PlanHash = tPlanHash
	ev := confirmEval(t, f)
	ev.evidenceSetHash, ev.policyVersions = "", nil
	res := run13(t, ev)
	if res.Decision.Outcome() != decide.Reject || f.called("Recorder.Record") != 1 || rec0(t, f).PlanHash != "" {
		t.Errorf("outcome %s, calls %v, plan hash on the record %q; want REJECT, one Record, no plan hash", res.Decision.Outcome(), f.calls, rec0(t, f).PlanHash)
	}
}

// A REJECT cannot be built without a basis or for an intent that is not resolved: nothing is recorded (the decision is built before the Recorder is called).
func TestT13NothingIsRecordedForADeclineWhenTheRejectCannotBeBuilt(t *testing.T) {
	for name, prep := range map[string]func(*testing.T, *evaluation){
		"no builder": func(_ *testing.T, e *evaluation) { e.bb = nil },
		"an unresolved intent": func(_ *testing.T, e *evaluation) {
			e.intent.View = decide.IntentView{Version: t13Version, MaterialFlags: []string{"AMBIGUOUS_COUNTERPARTY"}}
		},
	} {
		f := declinedAnswer(newFake13())
		ev := confirmEval(t, f)
		prep(t, ev)
		wantUnknown(t, name, run13(t, ev), "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
		wantRecordedNothing(t, name, f)
	}
}

// A refusal made before T13 keeps returning no decision ID: only a decision a step recorded names one.
func TestAREjectFromAnotherStepStillReturnsNoDecisionID(t *testing.T) {
	f := newFake13()
	ev := newEval13(t, f)
	var s decide.Steps
	for i := range s {
		s[i] = func(context.Context) decide.StepResult { return decide.Continue() }
	}
	s[decide.T4Capability-1] = func(context.Context) decide.StepResult {
		r, err := ev.resolved()
		if err != nil {
			return decide.FailStep(err)
		}
		reason, _ := decide.Core("PROHIBITED_CAPABILITY")
		d, err := r.Reject(decide.Authority{Technical: decide.Fail, Policy: decide.NotEvaluated, Business: decide.NotEvaluated}, reason)
		if err != nil {
			return decide.FailStep(err)
		}
		return decide.Stop(d)
	}
	res := ev.run(tctx(t), s)
	if res.Decision.Outcome() != decide.Reject || res.DecisionID != "" {
		t.Errorf("outcome %s, decision ID %q; want a REJECT with no decision ID", res.Decision.Outcome(), res.DecisionID)
	}
}
