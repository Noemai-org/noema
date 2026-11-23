package recorder_test

// A552 (interim, relayed, not yet committed; REQ-TRU-009, REQ-TRU-003, INV-25, EC-E16): the Writer writes the confirmation_request ROW of a CONFIRM in the same transaction as the
// decision and its two events: a CONFIRM leaves exactly one open request; a crash or a refusal between the decision and the request leaves neither; the expiry is the DATABASE clock plus
// the request's TTL; a fresh request after the first expired supersedes it and the chain stays linear; the Record event carries the expiry the row has. Real PostgreSQL, real tenant logins.
// All data is SYNTHETIC. The one real-time wait is the request's one-second TTL.

import (
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/confirm"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

type requestRow struct {
	id, correlation, intent, principal, capability, target, binding, requirement, requester string
	version                                                                                 int
	supersedes                                                                              *string
	requestedAt, expiresAt                                                                  time.Time
}

func (w *world) requestRows(t *testing.T) []requestRow {
	t.Helper()
	rows, err := w.admin.Query(ctx(t), `SELECT confirmation_request_id, correlation_id, intent_id, intent_version, principal_id, capability_id, target_ref, binding_hash, requirement, requester_id,
		supersedes_request_id, requested_at, expires_at FROM confirmation_request ORDER BY requested_at, confirmation_request_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []requestRow
	for rows.Next() {
		var r requestRow
		if err := rows.Scan(&r.id, &r.correlation, &r.intent, &r.version, &r.principal, &r.capability, &r.target, &r.binding, &r.requirement, &r.requester, &r.supersedes, &r.requestedAt, &r.expiresAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func (w *world) requestsAnswered(t *testing.T) int {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM confirmation_answer`)
}

// A CONFIRM with a request leaves exactly one open request, the row of the request the writer returned: the eight bound values and the binding hash the database recomputes, the intent
// and its version, the requirement and the requester; no answer; no predecessor. Its expiry is the database's clock plus the request's TTL (600 seconds here) and the
// CONFIRMATION_REQUESTED event carries the same expiry.
func TestAConfirmLeavesExactlyOneOpenRequestRow(t *testing.T) {
	w := newWorld(t)
	req := confirmRequest(t, w, "evs_01ABC", true)
	res, err := w.write(t, req, true)
	if err != nil {
		t.Fatal(err)
	}
	rows := w.requestRows(t)
	if len(rows) != 1 || w.requestsAnswered(t) != 0 {
		t.Fatalf("%d request rows and %d answers, want exactly one open request", len(rows), w.requestsAnswered(t))
	}
	r := rows[0]
	binding := bindingHashOf(t, confirm.BindingInput{PrincipalID: cfrPrincipal, IntentHash: letters(0), CapabilityID: capability, TargetRef: cfrTarget, ParamsHash: letters(1), EvidenceSetHash: letters(2),
		PlanHash: letters(3), PolicyVersion: policyVersion})
	if r.id != res.ConfirmationRequestID || r.correlation != cfrCorr || r.intent != "int_01J9X3" || r.version != req.Decision.IntentVersion() || r.principal != cfrPrincipal ||
		r.capability != capability || r.target != cfrTarget || r.binding != binding || r.requirement != "SELF" || r.requester != cfrPrincipal || r.supersedes != nil {
		t.Errorf("the request row %+v does not match the CONFIRM and the request %s", r, res.ConfirmationRequestID)
	}
	if ttl := r.expiresAt.Sub(r.requestedAt); ttl < 599*time.Second || ttl > 601*time.Second {
		t.Errorf("the request lives %v, want the request's TTL of 600 seconds on the database clock", ttl)
	}
	var near bool
	if err := w.admin.QueryRow(ctx(t), `SELECT abs(extract(epoch FROM (now() - requested_at))) < 60 FROM confirmation_request WHERE confirmation_request_id = $1`, r.id).Scan(&near); err != nil || !near {
		t.Errorf("requested_at %v is not the database's now() (%v)", r.requestedAt, err)
	}
	cr := w.eventsOf(t, "CONFIRMATION_REQUESTED")
	if len(cr) != 1 {
		t.Fatalf("%d CONFIRMATION_REQUESTED events", len(cr))
	}
	if got := payloadOf(t, cr[0])["expires_at"]; got != r.expiresAt.UTC().Format("2006-01-02T15:04:05.000Z") {
		t.Errorf("the event's expires_at is %v, the row's is %v", got, r.expiresAt.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
}

// EC-E16: a crash between the decision and the request, and a refusal of the request, leave neither: a rolled-back write leaves no request row either, and a request the database
// refuses (the action already has an open one) leaves the first decision alone and writes no second decision, event or row, with the typed error.
func TestACrashOrARefusalBetweenTheDecisionAndTheRequestLeavesNeither(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), false); err != nil {
		t.Fatal(err)
	}
	w.nothingWritten(t, "a rolled-back CONFIRM")
	if n := len(w.requestRows(t)); n != 0 {
		t.Errorf("a rolled-back CONFIRM left %d request rows", n)
	}

	if _, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true); err != nil {
		t.Fatal(err)
	}
	before := w.counts(t)
	rowsBefore := len(w.requestRows(t))
	second := confirmRequest(t, w, "evs_02XYZ", true) // another snapshot: another input, so a new decision and a new request, beside an open one
	if _, err := w.write(t, second, true); !errors.Is(err, recorder.ErrConfirmationRequestOpen) {
		t.Fatalf("a second request beside an open one: %v, want ErrConfirmationRequestOpen", err)
	}
	if after := w.counts(t); after != before || len(w.requestRows(t)) != rowsBefore {
		t.Errorf("the refusal changed the rows from %+v and %d requests to %+v and %d", before, rowsBefore, w.counts(t), len(w.requestRows(t)))
	}
}

// A re-request after expiry is a NEW CONFIRM decision (another input) and its request supersedes the expired one: the chain of the action stays linear, the first row is untouched, and the
// second names it. The first request lives one second on the database clock.
func TestAFreshConfirmAfterTheExpirySupersedesTheExpiredRequest(t *testing.T) {
	w := newWorld(t)
	first := confirmRequest(t, w, "evs_01ABC", true)
	first.ConfirmationRequest.ExpiresAt = epoch.Unix() + 1 // one second of TTL
	res1, err := w.write(t, first, true)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	res2, err := w.write(t, confirmRequest(t, w, "evs_02XYZ", true), true)
	if err != nil {
		t.Fatalf("the fresh request after the expiry: %v", err)
	}
	rows := w.requestRows(t)
	if len(rows) != 2 || rows[0].id != res1.ConfirmationRequestID || rows[1].id != res2.ConfirmationRequestID || rows[0].supersedes != nil || rows[1].supersedes == nil || *rows[1].supersedes != rows[0].id {
		t.Fatalf("the chain %+v, want %s then %s superseding it", rows, res1.ConfirmationRequestID, res2.ConfirmationRequestID)
	}
	if res1.Record.DecisionId == res2.Record.DecisionId {
		t.Error("the re-request is the first decision again; a re-request is a new CONFIRM decision")
	}
}

// A repeated Record of the same CONFIRM returns the stored decision and writes no second row; a CONFIRM that names an already open request (no request to create) writes none.
func TestARepeatedConfirmAndAConfirmWithNoRequestWriteNoSecondRow(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true); err != nil {
		t.Fatal(err)
	}
	if _, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true); err != nil {
		t.Fatal(err)
	}
	if n := len(w.requestRows(t)); n != 1 {
		t.Errorf("a repeated CONFIRM left %d request rows, want 1", n)
	}
	w2 := newWorld(t)
	if _, err := w2.write(t, confirmRequest(t, w2, "evs_01ABC", false), true); err != nil {
		t.Fatal(err)
	}
	if n := len(w2.requestRows(t)); n != 0 {
		t.Errorf("a CONFIRM with no request to create left %d request rows, want 0", n)
	}
}
