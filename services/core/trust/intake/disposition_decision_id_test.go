package intake

// TRU-10 (A503, interim; noema#733): the RecordRequest carries an optional DispositionDecisionID, the ESCALATE disposition (a decision with no step and no grant, recorded first through the
// Recorder, A470) that an escalation action decision links (PERD 23.1; REQ-TRU-010; INV-25). The Recorder's writer keeps its rule: a decision with escalation-set steps must name one
// (recorder.Request.DispositionDecisionID). Optional means every decision this version of the intake records leaves it empty: the escalation path (EL) records the disposition and passes
// its ID. The contract already has the member (decision-record.v1.json and v2: disposition_decision_id; migration 0012), so no contract change. All values are SYNTHETIC.

import (
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func dispositionDecisionIDOf(t *testing.T, r RecordRequest) string {
	t.Helper()
	f := reflect.ValueOf(r).FieldByName("DispositionDecisionID")
	if !f.IsValid() {
		t.Fatalf("RecordRequest has no DispositionDecisionID field (A503: an optional member, the ESCALATE disposition an escalation action decision links)")
	}
	if f.Kind() != reflect.String {
		t.Fatalf("DispositionDecisionID is a %s, want a string (a dec_ decision ID, the Recorder writer's type)", f.Kind())
	}
	return f.String()
}

// The field is optional: the zero RecordRequest has it empty.
func TestRecordRequestHasAnOptionalDispositionDecisionID(t *testing.T) {
	if got := dispositionDecisionIDOf(t, RecordRequest{}); got != "" {
		t.Errorf("the zero RecordRequest has DispositionDecisionID %q, want empty", got)
	}
}

// The intake itself records no escalation action decision: an ALLOW, a CONFIRM and a REJECT it records leave the field empty (the escalation path passes it, A503).
func TestTheDecisionsTheIntakeRecordsLeaveDispositionDecisionIDEmpty(t *testing.T) {
	allow := newFake13()
	if res := run13(t, newEval13(t, allow)); res.Decision.Outcome() != decide.Allow {
		t.Fatalf("outcome %s, want ALLOW", res.Decision.Outcome())
	}
	if got := dispositionDecisionIDOf(t, rec0(t, allow)); got != "" {
		t.Errorf("an ALLOW was recorded with DispositionDecisionID %q, want empty", got)
	}

	confirm := pendingOpen(newFake13())
	if res := run13(t, confirmEval(t, confirm)); res.Decision.Outcome() != decide.Confirm {
		t.Fatalf("outcome %s, want CONFIRM", res.Decision.Outcome())
	}
	if got := dispositionDecisionIDOf(t, rec0(t, confirm)); got != "" {
		t.Errorf("a CONFIRM was recorded with DispositionDecisionID %q, want empty", got)
	}

	declined := newFake13()
	declined.conf = Confirmation{Status: ConfirmationDeclined, RequestID: "cfr_synth0001"}
	declined.rec = RecordResult{DecisionID: "dec_synth0002"}
	if res := run13(t, newEval13(t, declined)); res.Decision.Outcome() != decide.Reject {
		t.Fatalf("outcome %s, want REJECT", res.Decision.Outcome())
	}
	if got := dispositionDecisionIDOf(t, rec0(t, declined)); got != "" {
		t.Errorf("a REJECT was recorded with DispositionDecisionID %q, want empty", got)
	}
}
