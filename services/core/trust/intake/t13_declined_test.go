package intake

// TRU-12 (REQ-TRU-009, REQ-NFR-002, INV-03, INV-25, EC-E16; SoT 43.2a; A435 and A456, interim, relayed, not yet committed): a Declined answer closes the decision as REJECT
// DECLINED_BY_APPROVER, and the REJECT is a decision, so it is recorded once, first, with no steps, no plan hash, no grant and no confirmation request. All data is SYNTHETIC.

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func TestT13DeclinedIsRejectDeclinedByApproverAndIsRecordedOnce(t *testing.T) {
	f := newFake13()
	f.conf = Confirmation{Status: ConfirmationDeclined, RequestID: "cfr_synth0001"}
	f.rec = RecordResult{DecisionID: "dec_synth0002"}
	res := run13(t, newEval13(t, f))
	if res.Decision.Outcome() != decide.Reject {
		t.Fatalf("outcome %s %v, want REJECT", res.Decision.Outcome(), codes13(res.Decision))
	}
	if cs := codes13(res.Decision); len(cs) != 1 || cs[0] != "DECLINED_BY_APPROVER" {
		t.Errorf("reasons %v, want DECLINED_BY_APPROVER", cs)
	}
	if res.ConfirmationRequestID != "" || len(res.GrantIDs) != 0 || res.DecisionID != "dec_synth0002" {
		t.Errorf("a decline carries a request id %q, decision %q (want the recorded one) or grants %v", res.ConfirmationRequestID, res.DecisionID, res.GrantIDs)
	}
	if !res.Decision.HasCompleteBasis() {
		t.Error("a decline does not carry the basis of the evaluation it closed")
	}
	if last := res.Trace[len(res.Trace)-1]; last.Step != decide.T13Confirmation || last.Result != decide.StepTerminal {
		t.Errorf("trace ends %+v, want T13 terminal", last)
	}
	if f.called("Recorder.Record") != 1 || f.called("Planner.Plan") != 0 {
		t.Fatalf("a decline must be recorded once and plan or request nothing: %v", f.calls)
	}
	r := rec0(t, f)
	if r.Decision.Outcome() != decide.Reject || len(r.Steps) != 0 || r.PlanHash != "" || r.ConfirmationRequest != nil || r.ConfirmationEventID != "" || r.ConfirmationPlanHash != "" {
		t.Errorf("recorded %s with %d steps, plan %q, confirmation %q %q; want the REJECT alone", r.Decision.Outcome(), len(r.Steps), r.PlanHash, r.ConfirmationEventID, r.ConfirmationPlanHash)
	}
	if r.TenantID != t13Tenant || r.PrincipalID != t13Principal || r.IntentID != tIntent || r.Evaluated == nil || r.BasisDigest == "" {
		t.Errorf("the record request lacks the session's identity, the intent or the evaluated set: %+v", r)
	}
	if got, err := r.Evaluated.Digest(); err != nil || got != r.BasisDigest {
		t.Errorf("basis digest %q, builder digest %q (%v)", r.BasisDigest, got, err)
	}
}

func TestT13DeclinedThatCannotBeRecordedIsUnknownNeverReject(t *testing.T) {
	for name, mod := range map[string]func(f *f13){
		"error":         func(f *f13) { f.recErr = errInjected },
		"hit error":     func(f *f13) { f.failAt = "Recorder.Record" },
		"panic":         func(f *f13) { f.panicAt = "Recorder.Record" },
		"no decision":   func(f *f13) { f.rec = RecordResult{} },
		"bad decision":  func(f *f13) { f.rec = RecordResult{DecisionID: "x"} },
		"a stray grant": func(f *f13) { f.rec = RecordResult{DecisionID: "dec_synth0002", GrantIDs: []string{"grt_synth0001"}} },
	} {
		f := newFake13()
		f.conf = Confirmation{Status: ConfirmationDeclined, RequestID: "cfr_synth0001"}
		f.rec = RecordResult{DecisionID: "dec_synth0002"}
		mod(f)
		res := run13(t, newEval13(t, f))
		if res.Decision.Outcome() != decide.Unknown || res.DecisionID != "" || len(res.GrantIDs) != 0 {
			t.Errorf("%s: outcome %s decision %q, want UNKNOWN with nothing recorded", name, res.Decision.Outcome(), res.DecisionID)
		}
		if cs := codes13(res.Decision); len(cs) != 1 || cs[0] != "INTERNAL_ERROR" {
			t.Errorf("%s: reasons %v, want INTERNAL_ERROR", name, cs)
		}
		if f.called("Planner.Plan") != 0 {
			t.Errorf("%s: planned or requested after a failed record: %v", name, f.calls)
		}
	}
}
