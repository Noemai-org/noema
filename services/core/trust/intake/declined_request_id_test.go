package intake

// TRU-12 (A470): the RecordRequest carries an optional DeclinedRequestID, so a REJECT DECLINED_BY_APPROVER can name the declined confirmation request (cfr_...) the Recorder's writer
// shape-checks (trust/recorder, Request.DeclinedRequestID). Optional means every decision this version of the intake records leaves it empty. REQ-TRU-002, REQ-TRU-009, INV-25, EC-E16.

import (
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func declinedRequestIDOf(t *testing.T, r RecordRequest) string {
	t.Helper()
	f := reflect.ValueOf(r).FieldByName("DeclinedRequestID")
	if !f.IsValid() {
		t.Fatalf("RecordRequest has no DeclinedRequestID field (A470: an optional member, empty for every decision but a REJECT DECLINED_BY_APPROVER)")
	}
	if f.Kind() != reflect.String {
		t.Fatalf("DeclinedRequestID is a %s, want a string (a cfr_ request ID)", f.Kind())
	}
	return f.String()
}

// The field is optional: the zero RecordRequest has it empty, and its type is the Recorder writer's (a string).
func TestRecordRequestHasAnOptionalDeclinedRequestID(t *testing.T) {
	if got := declinedRequestIDOf(t, RecordRequest{}); got != "" {
		t.Errorf("the zero RecordRequest has DeclinedRequestID %q, want empty", got)
	}
}

// Only a REJECT DECLINED_BY_APPROVER names a declined request: an ALLOW and a CONFIRM with an open request leave it empty.
func TestAnAllowAndAConfirmLeaveDeclinedRequestIDEmpty(t *testing.T) {
	allow := newFake13()
	res := run13(t, newEval13(t, allow))
	if res.Decision.Outcome() != decide.Allow {
		t.Fatalf("outcome %s, want ALLOW", res.Decision.Outcome())
	}
	if got := declinedRequestIDOf(t, rec0(t, allow)); got != "" {
		t.Errorf("an ALLOW was recorded with DeclinedRequestID %q, want empty", got)
	}

	confirm := pendingOpen(newFake13())
	res = run13(t, confirmEval(t, confirm))
	if res.Decision.Outcome() != decide.Confirm {
		t.Fatalf("outcome %s, want CONFIRM", res.Decision.Outcome())
	}
	if got := declinedRequestIDOf(t, rec0(t, confirm)); got != "" {
		t.Errorf("a CONFIRM was recorded with DeclinedRequestID %q, want empty", got)
	}
}

// A470, REQ-TRU-009, INV-25: a REJECT DECLINED_BY_APPROVER names the request that was declined, so the writer can store the decision with the request it rests on; the answer's own
// RequestID is that request.
func TestADeclinedRejectNamesTheDeclinedRequest(t *testing.T) {
	f := newFake13()
	f.conf = Confirmation{Status: ConfirmationDeclined, RequestID: "cfr_synth0001"}
	f.rec = RecordResult{DecisionID: "dec_synth0002"}
	res := run13(t, newEval13(t, f))
	if res.Decision.Outcome() != decide.Reject {
		t.Fatalf("outcome %s, want REJECT", res.Decision.Outcome())
	}
	if got := declinedRequestIDOf(t, rec0(t, f)); got != "cfr_synth0001" {
		t.Errorf("the REJECT was recorded with DeclinedRequestID %q, want the declined request cfr_synth0001", got)
	}

}

// A542 (interim, relayed, not yet committed), REQ-TRU-009, INV-03: a Declined answer must name the request it rests on, and the name has the shape of a confirmation request id,
// like the Given answer's. An answer with none, or a malformed one, is not an answer: UNKNOWN at T13 (as the Given path does), and nothing is recorded or planned.
func TestADeclinedAnswerWithoutAWellFormedRequestIDIsUnknownAndNotRecorded(t *testing.T) {
	for _, bad := range []string{"", "x", "cfr_", "cfr_a", "dec_synth0001", "cfr_synth 0001", "cfr_synth0001\n", "CFR_synth0001"} {
		f := newFake13()
		f.conf = Confirmation{Status: ConfirmationDeclined, RequestID: bad}
		f.rec = RecordResult{DecisionID: "dec_synth0002"}
		res := run13(t, newEval13(t, f))
		wantUnknown(t, "declined with request id "+bad, res, "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
		if f.called("Recorder.Record") != 0 || f.called("Planner.Plan") != 0 {
			t.Errorf("%q: the recorder or the planner was called: %v", bad, f.calls)
		}
	}
}
