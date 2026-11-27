package trustport

// TRU-12, Architect rulings A519 and A552 (interim, relayed, not yet committed): END-TO-END tests, written RED before the Writer change they wait for. The CONFIRM that the
// Recorder adapter records must leave its open confirmation_request row in the same transaction (REQ-TRU-009; INV-03: a decline, and later an ALLOW, rest on a request that exists), and
// the T13 decline (REJECT DECLINED_BY_APPROVER) must be able to name the request the CONFIRM minted. Everything is real: the adapter, the trust/recorder Writer and a PostgreSQL with every
// migration (no fake of either; the only seam is the transaction opener, as in recorder_test.go). All data is SYNTHETIC.
//
// History: written RED on 0c202c65, when the Writer's CONFIRM path wrote the decision and the CONFIRMATION_REQUESTED event and never a confirmation_request row. noema#911 (the Writer inserts
// the row in the CONFIRM transaction) has merged, so every test here is expected to PASS at the merged head. Test 3 (a decline naming another tenant's or another intent's request) guards the
// ownership check against a loosening green. These tests need PostgreSQL with pgsodium and run in CI only.

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const a519OtherTenant = "ten_bb"

// a519Expiry is the request's expiry: 5 minutes after the test's clock, within the 10-minute confirmation TTL ceiling (PERD REQ-TRU-009; A561, interim, relayed, not yet committed).
// The Writer turns it into a TTL against its own clock (the same manual clock, recEpoch) and stamps expires_at as the database's now() plus that TTL (migration 0032), so the request is
// open for 5 minutes of database time whatever date the manual clock shows. SYNTHETIC.
var a519Expiry = recEpoch.Add(5 * time.Minute).Unix()

// a519Pools opens each tenant's own transaction as its trust_rw login.
type a519Pools map[string]*pgxpool.Pool

func (p a519Pools) Begin(ctx context.Context, tenant string) (pgx.Tx, error) {
	pool, ok := p[tenant]
	if !ok {
		return nil, errors.New("not a test tenant")
	}
	return pool.Begin(ctx)
}

type a519World struct {
	*recWorld
	other *pgxpool.Pool // the other tenant's trust login
}

// newA519World is newRecWorld with a second tenant and the real adapter serving both.
func newA519World(t *testing.T) *a519World {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(recCtx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{recTenant, a519OtherTenant} {
		if _, err := admin.Exec(recCtx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
	}
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	keys, err := grants.NewKeys([]grants.Key{{ID: "k2", Secret: secret}})
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(recEpoch)
	idg := ids.New(clk, rand.New(rand.NewSource(1)))
	trust := testpg.TenantLogin(t, admin, dsn, "trust_rw", recTenant)
	other := testpg.TenantLogin(t, admin, dsn, "trust_rw", a519OtherTenant)
	a, err := NewRecorderAdapter(RecorderConfig{Begin: a519Pools{recTenant: trust, a519OtherTenant: other}, Facts: newRecFacts(), Keys: keys,
		Appender: &chain.Appender{Clock: clk, IDs: idg, Signers: recordsig.For(t, admin, "recorder", nil, "core-trust")},
		Clock:    clk, IDs: idg, Entropy: &recLockedReader{r: rand.New(rand.NewSource(2))}, GrantLifetime: 10 * time.Minute, RecordLifetime: 15 * time.Minute, ConfirmationTTLCeiling: 10 * time.Minute, TxTimeout: recTxTimeout})
	if err != nil {
		t.Fatal(err)
	}
	return &a519World{recWorld: &recWorld{admin: admin, trust: trust, reader: testpg.TenantLogin(t, admin, dsn, "record_read", recTenant), a: a}, other: other}
}

// confirmRequest is the T13 CONFIRM as the intake records it, with its new confirmation request.
func a519Confirm(t *testing.T, tenant string) intake.RecordRequest {
	t.Helper()
	r := rnaRequest(t, "CONFIRM", true)
	r.TenantID = tenant
	r.ConfirmationRequest.ExpiresAt = a519Expiry
	return r
}

// a519Decline is the T13 decline, built the way the intake builds it (trust/intake t13Declined): a REJECT with the reason DECLINED_BY_APPROVER through the decide constructors, the basis
// attached, and DeclinedRequestID naming the request the CONFIRM minted. It does not rest on rnaRequest, whose reason is not this test's to depend on (A552).
func a519Decline(t *testing.T, tenant, requestID string) intake.RecordRequest {
	t.Helper()
	b := recBuilder(t)
	res, ok := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	if !ok {
		t.Fatal("not resolved")
	}
	reason, err := decide.Core("DECLINED_BY_APPROVER")
	if err != nil {
		t.Fatal(err)
	}
	d, err := res.Reject(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}, reason)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = b.Attach(d); err != nil {
		t.Fatal(err)
	}
	digest, err := b.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return intake.RecordRequest{TenantID: tenant, CorrelationID: "corr_ABC123", PrincipalID: "usr_01J9X3ABCDEFGHJKMNPQRSTVWX", IntentID: "int_01J9X3", IntentHash: recHash(10), IntentVersion: 2,
		Decision: d, Evaluated: b.Sealed(), BasisDigest: digest, DeclinedRequestID: requestID}
}

// openRequests counts the open confirmation requests of a tenant's intent: not answered, not superseded, not expired at the database's clock (migration 0032).
func (w *a519World) openRequests(t *testing.T, tenant, intentID string) int {
	t.Helper()
	return w.count2(t, `SELECT count(*) FROM confirmation_request r WHERE r.tenant_id = $1 AND r.intent_id = $2 AND r.expires_at > now()
		AND NOT EXISTS (SELECT 1 FROM confirmation_answer a WHERE a.tenant_id = r.tenant_id AND a.confirmation_request_id = r.confirmation_request_id)
		AND NOT EXISTS (SELECT 1 FROM confirmation_request s WHERE s.tenant_id = r.tenant_id AND s.supersedes_request_id = r.confirmation_request_id)`, tenant, intentID)
}

func (w *a519World) count2(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(recCtx(t), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (w *a519World) written(t *testing.T) (records, events, requests int) {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM decision_record`), w.count(t, `SELECT count(*) FROM record_event`), w.count(t, `SELECT count(*) FROM confirmation_request`)
}

// REQ-TRU-009, INV-03: a CONFIRM recorded through the adapter leaves exactly one open confirmation_request row for its decision, the one the result names, bound to the decision's
// intent, and the chain still verifies.
func TestREQTRU009ConfirmRecordedThroughTheAdapterLeavesExactlyOneOpenRequestRow(t *testing.T) {
	w := newA519World(t)
	res, err := w.a.Record(recCtx(t), a519Confirm(t, recTenant))
	if err != nil || res.DecisionID == "" || res.ConfirmationRequestID == "" {
		t.Fatalf("the CONFIRM: %+v, %v", res, err)
	}
	if n := w.openRequests(t, recTenant, "int_01J9X3"); n != 1 {
		t.Fatalf("%d open confirmation requests for the intent after a CONFIRM, want exactly 1", n)
	}
	if n := w.count2(t, `SELECT count(*) FROM confirmation_request WHERE tenant_id = $1 AND confirmation_request_id = $2 AND intent_id = 'int_01J9X3' AND supersedes_request_id IS NULL`, recTenant, res.ConfirmationRequestID); n != 1 {
		t.Fatalf("the request %s the result names is not stored as the root request of the intent (%d rows)", res.ConfirmationRequestID, n)
	}
	if n := w.count2(t, `SELECT count(*) FROM record_event WHERE tenant_id = $1 AND event_type = 'CONFIRMATION_REQUESTED' AND payload->>'confirmation_request_id' = $2 AND payload->>'decision_id' = $3`,
		recTenant, res.ConfirmationRequestID, res.DecisionID); n != 1 {
		t.Fatalf("%d CONFIRMATION_REQUESTED events name the request and the decision, want 1", n)
	}
	if n, err := chain.Verify(recCtx(t), w.admin, recTenant); err != nil || n != 2 {
		t.Fatalf("chain verify: %d %v", n, err)
	}
}

// REQ-TRU-009, INV-03, EC-E16: the row is written in the CONFIRM's own transaction. When the row cannot be inserted (here: the action already has its open root request, which the
// database allows once, migration 0032), the decision, its event and the request are all rolled back, not the row alone.
func TestREQTRU009AFailedRequestRowInsertRollsBackTheConfirmDecision(t *testing.T) {
	w := newA519World(t)
	r := a519Confirm(t, recTenant)
	c := r.ConfirmationRequest
	binding, err := confirm.BindingHash(confirm.BindingInput{PrincipalID: c.PrincipalID, IntentHash: c.IntentHash, CapabilityID: c.CapabilityID, TargetRef: c.TargetRef, ParamsHash: c.ParamsHash,
		EvidenceSetHash: c.ConfirmationBindingEvidenceHash, PlanHash: c.PlanHash, PolicyVersion: c.PolicyVersion})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.trust.Exec(recCtx(t), `INSERT INTO confirmation_request (tenant_id, confirmation_request_id, correlation_id, intent_id, intent_version, principal_id, intent_hash, capability_id,
			target_ref, params_hash, evidence_set_hash, plan_hash, policy_version, binding_hash, requirement, requester_id, expires_at)
		VALUES ($1, 'cfr_01SEEDED', 'cor_1', $2, 1, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'SELF', $3, now() + interval '10 minutes')`,
		recTenant, r.IntentID, c.PrincipalID, c.IntentHash, c.CapabilityID, c.TargetRef, c.ParamsHash, c.ConfirmationBindingEvidenceHash, c.PlanHash, c.PolicyVersion, binding); err != nil {
		t.Fatalf("seed the open request: %v", err)
	}
	records, events, requests := w.written(t)
	if res, err := w.a.Record(recCtx(t), r); !errors.Is(err, recorder.ErrConfirmationRequestOpen) {
		t.Fatalf("a CONFIRM whose request row cannot be inserted: %+v, %v; want ErrConfirmationRequestOpen", res, err)
	}
	if r2, e2, q2 := w.written(t); r2 != records || e2 != events || q2 != requests {
		t.Fatalf("after the refused CONFIRM: %d decisions, %d events, %d requests; before: %d, %d, %d (the decision must roll back with the row)", r2, e2, q2, records, events, requests)
	}
}

// REQ-TRU-009, INV-03: the T13 decline names the request the CONFIRM minted (RecordResult.ConfirmationRequestID), is accepted, and its stored decision rests on that request.
func TestREQTRU009DeclineNamingTheRequestTheConfirmMintedIsAcceptedAndReferencesIt(t *testing.T) {
	w := newA519World(t)
	conf, err := w.a.Record(recCtx(t), a519Confirm(t, recTenant))
	if err != nil || conf.ConfirmationRequestID == "" {
		t.Fatalf("the CONFIRM: %+v, %v", conf, err)
	}
	dec, err := w.a.Record(recCtx(t), a519Decline(t, recTenant, conf.ConfirmationRequestID))
	if err != nil || dec.DecisionID == "" || len(dec.GrantIDs) != 0 {
		t.Fatalf("the decline naming the minted request %s: %+v, %v", conf.ConfirmationRequestID, dec, err)
	}
	var state string
	var outcome string
	if err := w.admin.QueryRow(recCtx(t), `SELECT outcome, COALESCE(confirmation_request_id, '') FROM decision_record WHERE tenant_id = $1 AND decision_id = $2`, recTenant, dec.DecisionID).Scan(&outcome, &state); err != nil {
		t.Fatal(err)
	}
	if outcome != "REJECT" || state != conf.ConfirmationRequestID {
		t.Fatalf("the stored decline is %s resting on %q, want REJECT resting on %s", outcome, state, conf.ConfirmationRequestID)
	}
	if n, err := chain.Verify(recCtx(t), w.admin, recTenant); err != nil || n != 3 {
		t.Fatalf("chain verify: %d %v", n, err)
	}
}

// REQ-TRU-009, REQ-SEC-004, INV-03: a decline that names a request of another tenant, or of another intent, is refused with ErrDeclinedRequestNotValid and nothing is written. Both requests
// are real (minted by a CONFIRM through the adapter), so the refusal is the ownership check and not an absent row.
func TestREQTRU009DeclineNamingAnotherTenantsOrAnotherIntentsRequestIsRefusedAndNothingIsWritten(t *testing.T) {
	w := newA519World(t)
	mine, err := w.a.Record(recCtx(t), a519Confirm(t, recTenant))
	if err != nil || mine.ConfirmationRequestID == "" {
		t.Fatalf("the CONFIRM of the tenant: %+v, %v", mine, err)
	}
	theirs, err := w.a.Record(recCtx(t), a519Confirm(t, a519OtherTenant))
	if err != nil || theirs.ConfirmationRequestID == "" {
		t.Fatalf("the CONFIRM of the other tenant: %+v, %v", theirs, err)
	}
	other := a519Decline(t, recTenant, mine.ConfirmationRequestID)
	other.IntentID = "int_01OTHER"
	cases := []struct {
		name string
		r    intake.RecordRequest
	}{
		{"another tenant's request", a519Decline(t, recTenant, theirs.ConfirmationRequestID)},
		{"another intent's request", other},
	}
	for _, c := range cases {
		records, events, requests := w.written(t)
		if _, err := w.a.Record(recCtx(t), c.r); !errors.Is(err, recorder.ErrDeclinedRequestNotValid) {
			t.Errorf("%s: err %v, want ErrDeclinedRequestNotValid", c.name, err)
		}
		if r2, e2, q2 := w.written(t); r2 != records || e2 != events || q2 != requests {
			t.Errorf("%s: %d decisions, %d events, %d requests after the refusal, were %d, %d, %d", c.name, r2, e2, q2, records, events, requests)
		}
	}
}

// REQ-TRU-009, INV-03, INV-21: a replay of the CONFIRM returns the stored decision and creates no second request: still exactly one open request, no new decision, no new event.
func TestREQTRU009ReplayOfAConfirmDoesNotCreateASecondOpenRequest(t *testing.T) {
	w := newA519World(t)
	first, err := w.a.Record(recCtx(t), a519Confirm(t, recTenant))
	if err != nil || first.ConfirmationRequestID == "" {
		t.Fatalf("the CONFIRM: %+v, %v", first, err)
	}
	records, events, requests := w.written(t)
	second, err := w.a.Record(recCtx(t), a519Confirm(t, recTenant))
	if err != nil || second.DecisionID != first.DecisionID {
		t.Fatalf("the replay: %+v, %v; want the stored decision %s", second, err, first.DecisionID)
	}
	if r2, e2, q2 := w.written(t); r2 != records || e2 != events || q2 != requests {
		t.Fatalf("after the replay: %d decisions, %d events, %d requests; before: %d, %d, %d", r2, e2, q2, records, events, requests)
	}
	if n := w.openRequests(t, recTenant, "int_01J9X3"); n != 1 {
		t.Fatalf("%d open requests after a replay, want exactly 1", n)
	}
}
