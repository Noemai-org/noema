package recorder_test

// TRU-12, ruling A507 (interim; the Security Reviewer's condition on noema#849 and #840): the declined request a REJECT DECLINED_BY_APPROVER names is checked for ownership before it is
// written: it must be a confirmation request of this tenant (row security hides another tenant's; the tenant is in the predicate too) and of this decision's intent. A well-formed ID
// that names no request, or another tenant's, or another intent's, is refused with ErrDeclinedRequestNotValid and nothing is written. The existing form check (cfr_ and the ID's
// characters) and the rule that no other outcome names a request stay. REQ-TRU-009, REQ-SEC-004; INV-25, INV-12, EC-E16. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// openRequest records a CONFIRM that creates a confirmation request for the intent of the tuple, as the Trust login of the tenant, and returns the request's ID.
func (w *world) openRequest(t *testing.T) string {
	t.Helper()
	res, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true)
	if err != nil || res.ConfirmationRequestID == "" {
		t.Fatalf("the CONFIRM that creates the request: %+v, %v", res, err)
	}
	return res.ConfirmationRequestID
}

// recordAndEventCounts is what a refused decline must leave as it was: the decisions and the events.
func (w *world) recordAndEventCounts(t *testing.T) (records, events int) {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM decision_record`), w.count(t, `SELECT count(*) FROM record_event`)
}

func TestADeclinedRejectNamingTheRealRequestOfItsIntentIsStoredWithItAsItsState(t *testing.T) { // A507: the real one
	w := newWorld(t)
	id := w.openRequest(t)
	res, err := w.write(t, declinedReject(t, "evs_01ABC", id), true)
	if err != nil {
		t.Fatalf("a decline naming the request of its own intent: %v", err)
	}
	if s, null := w.stateOf(t, string(res.Record.DecisionId)); null || s != id {
		t.Fatalf("the REJECT rests on state %q (null %v), want the request %s", s, null, id)
	}
}

func TestADeclinedRejectNamingARequestThatDoesNotExistIsRefusedAndNothingIsWritten(t *testing.T) { // A507: none at all
	w := newWorld(t)
	w.openRequest(t)
	records, events := w.recordAndEventCounts(t)
	_, err := w.write(t, declinedReject(t, "evs_01ABC", "cfr_01NOSUCH"), true)
	if !errors.Is(err, recorder.ErrDeclinedRequestNotValid) {
		t.Fatalf("err %v, want ErrDeclinedRequestNotValid", err)
	}
	if r2, e2 := w.recordAndEventCounts(t); r2 != records || e2 != events {
		t.Fatalf("%d records and %d events after the refusal, were %d and %d", r2, e2, records, events)
	}
}

func TestADeclinedRejectNamingARequestOfAnotherIntentIsRefusedAndNothingIsWritten(t *testing.T) { // A507: another intent's
	w := newWorld(t)
	id := w.openRequest(t)
	records, events := w.recordAndEventCounts(t)
	r := declinedReject(t, "evs_01ABC", id)
	r.IntentID = "int_01OTHER"
	if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrDeclinedRequestNotValid) {
		t.Fatalf("err %v, want ErrDeclinedRequestNotValid", err)
	}
	if r2, e2 := w.recordAndEventCounts(t); r2 != records || e2 != events {
		t.Fatalf("%d records and %d events after the refusal, were %d and %d", r2, e2, records, events)
	}
}

func TestADeclinedRejectNamingARequestOfAnotherTenantIsRefusedAndNothingIsWritten(t *testing.T) { // A507: another tenant's
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	other := confirmRequest(t, w, "evs_01ABC", true)
	other.TenantID = "ten_bb"
	otherLogin := testpg.TenantLogin(t, w.admin, w.dsn, "trust_rw", "ten_bb")
	res, err := w.writeVia(t, otherLogin, other)
	if err != nil || res.ConfirmationRequestID == "" {
		t.Fatalf("the other tenant's request: %+v, %v", res, err)
	}
	records, events := w.recordAndEventCounts(t)
	if _, err := w.write(t, declinedReject(t, "evs_01ABC", res.ConfirmationRequestID), true); !errors.Is(err, recorder.ErrDeclinedRequestNotValid) {
		t.Fatalf("err %v, want ErrDeclinedRequestNotValid for another tenant's request", err)
	}
	if r2, e2 := w.recordAndEventCounts(t); r2 != records || e2 != events {
		t.Fatalf("%d records and %d events after the refusal, were %d and %d", r2, e2, records, events)
	}
}

// A decline that names no request stays valid (the field is optional, A470) and a name that is not a request ID is still refused as a bad request, not as an ownership failure.
func TestADeclineThatNamesNoRequestStaysValidAndAMalformedNameStaysABadRequest(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, declinedReject(t, "evs_01ABC", ""), true); err != nil {
		t.Fatalf("a decline that names no request: %v", err)
	}
	w2 := newWorld(t)
	if _, err := w2.write(t, declinedReject(t, "evs_01ABC", "not_a_request"), true); !errors.Is(err, recorder.ErrRequest) || errors.Is(err, recorder.ErrDeclinedRequestNotValid) {
		t.Fatalf("a malformed name: %v, want ErrRequest and not the ownership error", err)
	}
}
