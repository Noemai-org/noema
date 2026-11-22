package recorder_test

// TRU-12, ruling A507 (interim; the Security Reviewer's condition on noema#849 and #840): the declined request a REJECT DECLINED_BY_APPROVER names is checked for ownership before it is
// written: it must be a confirmation request of this tenant (row security hides another tenant's; the tenant is in the predicate too) and of this decision's intent. A well-formed ID
// that names no request, or another tenant's, or another intent's, is refused with ErrDeclinedRequestNotValid and nothing is written. The existing form check (cfr_ and the ID's
// characters) and the rule that no other outcome names a request stay. REQ-TRU-009, REQ-SEC-004; INV-25, INV-12, EC-E16. All data is SYNTHETIC.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// seedRequest stores a confirmation request row as the confirmation store does (orchestrator/portadapters/trustpg2: the Writer's CONFIRM path writes the decision and the
// CONFIRMATION_REQUESTED event and no row, so the row a decline rests on is the store's), as the Trust login of the tenant, for the intent. The binding hash is the canonical form of
// the eight bound values, which the database recomputes and refuses on any difference. Each request is for a target of its own (one root request per action).
func (w *world) seedRequest(t *testing.T, pool *pgxpool.Pool, tenantID, id, intentID string) {
	t.Helper()
	b := struct{ capability, evidence, intent, params, plan, policy, principal, target string }{"cap.change", "sha256:e1", "sha256:i1", "sha256:p1", "sha256:l1", "pol_v1", "prn_1", "fin.transaction:" + id} // one root request per action: each request is for a target of its own
	canonical := `{"capability_id":"` + b.capability + `","evidence_set_hash":"` + b.evidence + `","intent_hash":"` + b.intent + `","params_hash":"` + b.params + `","plan_hash":"` + b.plan +
		`","policy_version":"` + b.policy + `","principal_id":"` + b.principal + `","target_ref":"` + b.target + `"}`
	sum := sha256.Sum256([]byte(canonical))
	if _, err := pool.Exec(ctx(t), `INSERT INTO confirmation_request (tenant_id, confirmation_request_id, correlation_id, intent_id, intent_version, principal_id, intent_hash, capability_id,
			target_ref, params_hash, evidence_set_hash, plan_hash, policy_version, binding_hash, requirement, requester_id, expires_at)
		VALUES ($1, $2, 'cor_1', $3, 1, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'SELF', 'prn_1', now() + interval '10 minutes')`,
		tenantID, id, intentID, b.principal, b.intent, b.capability, b.target, b.params, b.evidence, b.plan, b.policy, "sha256:"+hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("seed the request %s: %v", id, err)
	}
}

// openRequest seeds the request of the tenant's own intent and returns its ID.
func (w *world) openRequest(t *testing.T) string {
	t.Helper()
	const id = "cfr_01REAL"
	w.seedRequest(t, w.trust, tenant, id, "int_01J9X3")
	return id
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
	w.seedRequest(t, testpg.TenantLogin(t, w.admin, w.dsn, "trust_rw", "ten_bb"), "ten_bb", "cfr_01BBREQ", "int_01J9X3")
	records, events := w.recordAndEventCounts(t)
	if _, err := w.write(t, declinedReject(t, "evs_01ABC", "cfr_01BBREQ"), true); !errors.Is(err, recorder.ErrDeclinedRequestNotValid) {
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
