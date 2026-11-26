package trustpg2_test

// A710 and A724 (interim, relayed; REQ-TRU-009; INV-12, INV-25; PERD 32.4): the confirmation store's Record events are appended in the STORE'S OWN transaction by a real appender, so
// the answer row and the event commit together or not at all. The actor of CONFIRMATION_RECEIVED is the answerer with the kind the session gave; CONFIRMATION_EXPIRED is the core's own.
// All data is SYNTHETIC.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg2"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

type recEvent struct{ eventType, actor, objectRef, writtenBy, payload string }

func (w *reqWorld) recorded(t *testing.T) []recEvent {
	t.Helper()
	rows, err := w.admin.Query(reqCtx(t), `SELECT event_type, actor, coalesce(object_ref, ''), written_by, coalesce(payload::text, '') FROM record_event
		WHERE event_type LIKE 'CONFIRMATION_%' ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []recEvent
	for rows.Next() {
		var e recEvent
		if err := rows.Scan(&e.eventType, &e.actor, &e.objectRef, &e.writtenBy, &e.payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// recReq is newReq with a correlation ID of the Record's own form (corr_ and a short ID): the real appender refuses the sink tests' "cor_1".
func (w *reqWorld) recReq(plan string, ttl time.Duration, supersedes string) trustpg2.NewRequest {
	n := w.newReq(plan, ttl, supersedes)
	n.CorrelationID = "corr_Rec1"
	return n
}

// realStore is the store of tenant A with the real Record appender over the same database.
func (w *reqWorld) realStore(t *testing.T, signed bool) trustpg2.Requests {
	t.Helper()
	app := &chain.Appender{Clock: w.clk, IDs: w.a.IDs}
	if signed {
		app.Signers = recordsig.For(t, w.admin, "trustpg2-events", nil, "core-trust")
	}
	r := w.a
	r.Events = trustpg2.RecordEvents{Appender: app}
	return r
}

func TestAnswerAppendsConfirmationReceivedInTheStoresTransaction(t *testing.T) {
	w := newReqWorld(t)
	id, err := w.a.Open(reqCtx(t), w.recReq("sha256:r1", time.Hour, ""))
	if err != nil {
		t.Fatal(err)
	}
	store := w.realStore(t, true)
	event, err := store.Answer(reqCtx(t), trustpg2.AnswerInput{TenantID: "ten_aa", RequestID: id, ApproverID: "prn_2", ApproverKind: "approver", Roles: []string{"approver"}, Response: confirm.Accept})
	if err != nil {
		t.Fatal(err)
	}
	got := w.recorded(t)
	if len(got) != 1 || got[0].eventType != "CONFIRMATION_RECEIVED" || got[0].actor != "approver:prn_2" || got[0].objectRef != id || got[0].writtenBy != "core-trust" {
		t.Fatalf("Record events = %+v, want one CONFIRMATION_RECEIVED by approver:prn_2 about %s written by core-trust", got, id)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(got[0].payload), &payload); err != nil || payload["confirmation_id"] != event || payload["accepted"] != true || payload["confirmation_request_id"] != id {
		t.Fatalf("payload %s must carry the request ID %s, the confirmation event ID %s and accepted (%v)", got[0].payload, id, event, err)
	}
}

func TestAnAppendThatFailsStoresNoAnswer(t *testing.T) {
	w := newReqWorld(t)
	id, err := w.a.Open(reqCtx(t), w.recReq("sha256:r2", time.Hour, ""))
	if err != nil {
		t.Fatal(err)
	}
	store := w.realStore(t, false) // no signer for core-trust: the append fails before it writes
	if _, err := store.Answer(reqCtx(t), trustpg2.AnswerInput{TenantID: "ten_aa", RequestID: id, ApproverID: "prn_2", ApproverKind: "approver", Roles: []string{"approver"}, Response: confirm.Accept}); err == nil {
		t.Fatal("the answer was stored although the Record refused the event")
	}
	if n := w.rows(t, "confirmation_answer"); n != 0 {
		t.Fatalf("confirmation_answer rows = %d, want 0 (record first, EC-E16)", n)
	}
	if got := w.recorded(t); len(got) != 0 {
		t.Fatalf("Record events = %+v, want none", got)
	}
}

func TestAnAnswerNeedsAKindThatIsAPrincipalKind(t *testing.T) {
	w := newReqWorld(t)
	id, err := w.a.Open(reqCtx(t), w.recReq("sha256:r3", time.Hour, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"", "Approver", "app rover", "core-trust"} {
		_, err := w.realStore(t, true).Answer(reqCtx(t), trustpg2.AnswerInput{TenantID: "ten_aa", RequestID: id, ApproverID: "prn_2", ApproverKind: kind, Roles: []string{"approver"}, Response: confirm.Accept})
		if err == nil {
			t.Errorf("kind %q: the answer was stored", kind)
		}
	}
	if n := w.rows(t, "confirmation_answer"); n != 0 {
		t.Fatalf("confirmation_answer rows = %d, want 0", n)
	}
}

func TestExpireAppendsConfirmationExpiredAsTheCoresOwnEvent(t *testing.T) {
	w := newReqWorld(t)
	id, err := w.a.Open(reqCtx(t), w.recReq("sha256:r4", time.Second, ""))
	if err != nil {
		t.Fatal(err)
	}
	w.waitExpired(t, id)
	if err := w.realStore(t, true).Expire(reqCtx(t), "ten_aa", id); err != nil {
		t.Fatal(err)
	}
	got := w.recorded(t)
	if len(got) != 1 || got[0].eventType != "CONFIRMATION_EXPIRED" || got[0].actor != "core-trust" || got[0].objectRef != id {
		t.Fatalf("Record events = %+v, want one CONFIRMATION_EXPIRED by core-trust about %s", got, id)
	}
}
