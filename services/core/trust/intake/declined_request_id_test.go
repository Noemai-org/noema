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
// RequestID is that request. An answer that names no request leaves it empty (the field is optional).
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

	none := newFake13()
	none.conf = Confirmation{Status: ConfirmationDeclined}
	none.rec = RecordResult{DecisionID: "dec_synth0002"}
	if res = run13(t, newEval13(t, none)); res.Decision.Outcome() != decide.Reject {
		t.Fatalf("outcome %s, want REJECT", res.Decision.Outcome())
	}
	if got := declinedRequestIDOf(t, rec0(t, none)); got != "" {
		t.Errorf("a decline that names no request was recorded with DeclinedRequestID %q, want empty", got)
	}
}
