package rowreader_test

// EXP-01 / A710, A675 (the lane-5 reader pattern: tenant in the WHERE, row security forced, the tenant's own Trust login, one read-only transaction): the Reader over a real database.
// These need a PostgreSQL server with pgsodium (NOEMA_TEST_DATABASE_URL, or the binaries on the machine) and the migrations applied; CI runs them. Names start with TestDB so that a
// machine with no server can leave them out with -skip TestDB. Every row is SYNTHETIC. REQ-EXP-001, REQ-SEC-004; INV-04, INV-07, INV-11, INV-12, INV-21; AGENT-13.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/explanation/factsource/rowreader"
	"github.com/Noemai-org/noema/services/core/internal/sigfixture"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

const (
	tenantB    = "ten_bb"
	subjectKey = "subject_resource"
	node       = "syn.node.investigate"
	snapID     = "evs_deccase01" // the snapshot the decision names: "evs_" and the decision id's letters and digits (testpg.DecisionDocJSON)
	grantOne   = "grt_0001"
	receiptOne = "rcp_0001"
)

var sealedAt = time.Date(2026, 11, 12, 9, 40, 0, 0, time.UTC)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

type dbWorld struct {
	evidence map[string]*pgxpool.Pool
	dsn      string
	admin    *pgxpool.Pool
	trust    map[string]*pgxpool.Pool
	key      sigfixture.Key
	reader   *rowreader.Reader
}

// newDBWorld migrates a database with two tenants and gives tenant A one ALLOW decision, its sealed snapshot (classification EXECUTION_OUTCOME_UNKNOWN), its admitted intent (the subject slot
// resolved to txnRef) and nothing else: each test adds the grant, receipt and verdict it needs.
func newDBWorld(t *testing.T) *dbWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	w := &dbWorld{evidence: map[string]*pgxpool.Pool{}, dsn: dsn, admin: admin, trust: map[string]*pgxpool.Pool{}, key: sigfixture.NewKey("rowreader", 1)}
	sigfixture.Register(t, admin, w.key)
	evidence := w.evidence
	for _, id := range []string{tenantA, tenantB} {
		w.exec(t, `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id)
		evidence[id] = testpg.TenantLogin(t, admin, dsn, "evidence_rw", id)
		w.trust[id] = testpg.TenantLogin(t, admin, dsn, "trust_rw", id)
	}
	w.seal(t, evidence[tenantA], tenantA, snapID, "EXECUTION_OUTCOME_UNKNOWN")
	w.intent(t, tenantA, "int_synth01", testpg.FixtureIntentHash, string(txnRef))
	w.decision(t, tenantA, decision, "evs_deccase01")
	r, err := rowreader.NewReader(rowreader.PoolsOf(w.trust), 30*time.Second, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	w.reader = r
	return w
}

func (w *dbWorld) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := w.admin.Exec(ctx(t), q, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, q)
	}
}

// seal inserts one evidence item as the tenant's Evidence login and the snapshot over it as its Trust login.
func (w *dbWorld) seal(t *testing.T, evidence *pgxpool.Pool, tenant, snapshotID, class string) {
	tag := "_" + tenant
	t.Helper()
	hash := fmt.Sprintf("sha256:%064x", 0xabc1)
	if _, err := evidence.Exec(ctx(t), `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at) VALUES ('pay_rowreader' || $4::text, $1, '{"synthetic": true}', $2, 'S2_internal', $3)`, tenant, hash, sealedAt, tag); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if _, err := evidence.Exec(ctx(t), `INSERT INTO evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at,
		freshness_sla_seconds, authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at)
		VALUES ('evr_rowreader' || $5::text, $1, $2, 'syn.src.ledger', 'rec-1', 'TRUST', $3, $3, 300, 'NONE', 'res_synthetic', 'prn_synthetic', 'S2_internal', 'KNOWN', $4, 'pay_rowreader' || $5::text, $3)`, tenant, corr, sealedAt, hash, tag); err != nil {
		t.Fatalf("item: %v", err)
	}
	set, err := snapshot.SetHash([]snapshot.Pair{{EvidenceID: "evr_rowreader" + tag, PayloadHash: hash}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.trust[tenant].Exec(ctx(t), `INSERT INTO context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at)
		VALUES ($1, $2, $3, ARRAY['evr_rowreader' || $7::text], 'rec_rowreader', $4, $5, $6)`, snapshotID, tenant, corr, class, set, sealedAt, tag); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
}

// intent plants the admitted intent as the migrating role would see it (the orchestrator's insert is not this reader's concern).
func (w *dbWorld) intent(t *testing.T, tenant, intentID, hash, subject string) {
	t.Helper()
	doc := map[string]any{"intent_id": intentID, "tenant_id": tenant, "correlation_id": corr, "version": 1, "status": "VALIDATED", "intent_node": node,
		"slots": map[string]any{subjectKey: map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": subject}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	w.exec(t, `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json, created_at) VALUES ($1, $2, 1, NULL, $3, $4, $5, $6::jsonb, $7)`,
		tenant, intentID, node, hash, corr, string(raw), sealedAt)
}

func (w *dbWorld) decision(t *testing.T, tenant, id, snapshot string) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(testpg.DecisionDocJSON(id, tenant, testpg.FixtureIntentHash, testpg.FixturePolicyVersion, corr)), &doc); err != nil {
		t.Fatal(err)
	}
	doc["evidence_snapshot_id"] = snapshot
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	w.exec(t, `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value) VALUES ($1, $2, $3::jsonb, 'ALLOW', $4, '[]'::jsonb, 'k1', 'aa')`,
		id, tenant, string(raw), testpg.FixtureIntentHash)
}

// grant plants a spent grant of the decision, with its capability and correlation in the signed document.
func (w *dbWorld) grant(t *testing.T, tenant, decisionID, grantID, capability, correlation string) {
	t.Helper()
	w.exec(t, `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		VALUES ($1, $2, $3, 'n_' || $1, jsonb_build_object('correlation_id', $4::text, 'principal_id', 'usr_100', 'decision_id', $3::text, 'tenant_id', $2::text, 'capability_id', $5::text) || `+testpg.GrantBoundSQL+`, 'p', '1', now() + interval '1 hour', 'k', 's')`,
		grantID, tenant, decisionID, correlation, capability)
	w.exec(t, `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ($1, $2, 'n_' || $1, 'idk_v1_' || $1, 1, now())`, grantID, tenant)
}

func (w *dbWorld) receipt(t *testing.T, tenant, grantID, receiptID, state, ack string) {
	t.Helper()
	sigfixture.InsertReceipt(t, w.admin, w.key, sigfixture.Receipt{ID: receiptID, Tenant: tenant, Grant: grantID, IdempotencyKey: "idk_v1_" + grantID, LogicalAction: "CREATE_CASE", State: state,
		Raw: "OK", Ack: ack, Correlation: corr, Attempt: 1, Dispatched: sealedAt})
}

func (w *dbWorld) verdict(t *testing.T, tenant, id, receiptID, outcome string, at time.Time) {
	t.Helper()
	w.exec(t, `INSERT INTO outcome_verdict (verdict_id, tenant_id, correlation_id, receipt_id, verification_id, outcome_definition, verdict, reason, predicate_result, evidence_reads, verified_at, json, sig_key_id, sig_value)
		VALUES ($1, $2, $3, $4, 'vrf_' || $1, $5, 'ACHIEVED', 'synthetic', true, ARRAY['evr_x'], $6, '{}', 'k', 's')`, id, tenant, corr, receiptID, outcome, at)
}

// plant is the whole write path of one decision: grant, receipt and one verdict.
func (w *dbWorld) plant(t *testing.T) {
	t.Helper()
	w.grant(t, tenantA, decision, grantOne, caseCap, corr)
	w.receipt(t, tenantA, grantOne, receiptOne, "ACKNOWLEDGED", ack)
	w.verdict(t, tenantA, "ver_0001", receiptOne, caseOut, sealedAt.Add(time.Minute))
}

// The rows the database holds are what the reader returns: the decision's correlation and sealed classification, the subject of its intent, and the grant, receipt and verdict of the case.
func TestDBTheReaderReturnsWhatTheRowsHold(t *testing.T) {
	w := newDBWorld(t)
	w.plant(t)
	got, err := w.reader.Read(ctx(t), tenantA, decision)
	if err != nil {
		t.Fatal(err)
	}
	if got.DecisionID != decision || got.CorrelationID != corr || got.Classification != "EXECUTION_OUTCOME_UNKNOWN" || got.SubjectRef != string(txnRef) || len(got.Executions) != 1 {
		t.Fatalf("rows = %+v", got)
	}
	e := got.Executions[0]
	if e.GrantID != grantOne || e.CapabilityID != caseCap || e.ReceiptID != receiptOne || e.State != "ACKNOWLEDGED" || e.ExternalAck == nil || *e.ExternalAck != ack || !e.OwnGrant ||
		e.Verdict != "ACHIEVED" || e.OutcomeID != caseOut {
		t.Fatalf("execution = %+v", e)
	}
}

// A grant with no receipt, or a receipt with no verdict, is returned as such (the Source then makes no case reference): the rows are not invented.
func TestDBAGrantWithNoReceiptAndAReceiptWithNoVerdictAreReturnedAsTheyAre(t *testing.T) {
	w := newDBWorld(t)
	w.grant(t, tenantA, decision, grantOne, caseCap, corr)
	got, err := w.reader.Read(ctx(t), tenantA, decision)
	if err != nil || len(got.Executions) != 1 || got.Executions[0].ReceiptID != "" || got.Executions[0].Verdict != "" {
		t.Fatalf("grant only: %+v %v", got, err)
	}
	w.receipt(t, tenantA, grantOne, receiptOne, "OUTCOME_UNKNOWN", "")
	got, err = w.reader.Read(ctx(t), tenantA, decision)
	if err != nil || len(got.Executions) != 1 || got.Executions[0].ReceiptID != receiptOne || got.Executions[0].State != "OUTCOME_UNKNOWN" || got.Executions[0].ExternalAck != nil || got.Executions[0].Verdict != "" {
		t.Fatalf("receipt only: %+v %v", got, err)
	}
}

// A grant of another decision of the same investigation is read, and is not the decision's own: the case receipt may belong to the create-case decision while the question is about its
// disposition (J3), and the outcome is never taken from it.
func TestDBAnotherDecisionsGrantOfTheInvestigationIsNotTheDecisionsOwn(t *testing.T) {
	w := newDBWorld(t)
	w.decision(t, tenantA, "dec_other01", "evs_decother01")
	w.grant(t, tenantA, "dec_other01", "grt_0002", caseCap, corr)
	got, err := w.reader.Read(ctx(t), tenantA, decision)
	if err != nil || len(got.Executions) != 1 || got.Executions[0].GrantID != "grt_0002" || got.Executions[0].OwnGrant {
		t.Fatalf("%+v %v", got, err)
	}
}

// When a receipt has two verdicts (a watch check after the first), the latest is the current one; two at one instant that disagree are ambiguous, never a guess.
func TestDBTheLatestVerdictIsTheCurrentOneAndATieIsAmbiguous(t *testing.T) {
	w := newDBWorld(t)
	w.plant(t)
	w.verdict(t, tenantA, "ver_0002", receiptOne, "syn.outcome.case_routed", sealedAt.Add(2*time.Minute))
	got, err := w.reader.Read(ctx(t), tenantA, decision)
	if err != nil || got.Executions[0].OutcomeID != "syn.outcome.case_routed" {
		t.Fatalf("latest: %+v %v", got, err)
	}
	w.verdict(t, tenantA, "ver_0003", receiptOne, "syn.outcome.case_other", sealedAt.Add(2*time.Minute))
	if got, err := w.reader.Read(ctx(t), tenantA, decision); !errors.Is(err, rowreader.ErrAmbiguous) || len(got.Executions) != 0 {
		t.Fatalf("tie: %+v %v", got, err)
	}
}

// REQ-SEC-004, INV-11: tenant B's login cannot read tenant A's decision, with the tenant named correctly or not; the answer is the one for a decision that does not exist, and a reader
// wired with A's login under B's name (a wiring fault) still finds nothing, because the database binds the tenant, not the code.
func TestDBAnotherTenantsDecisionIsNotFound(t *testing.T) {
	w := newDBWorld(t)
	w.plant(t)
	_, foreign := w.reader.Read(ctx(t), tenantB, decision)
	_, unknown := w.reader.Read(ctx(t), tenantB, "dec_nosuch01")
	if !errors.Is(foreign, rowreader.ErrNotFound) || !errors.Is(unknown, rowreader.ErrNotFound) || foreign.Error() != unknown.Error() {
		t.Fatalf("foreign %v, unknown %v; want the same ErrNotFound", foreign, unknown)
	}
	miswired, err := rowreader.NewReader(map[string]rowreader.Beginner{tenantB: w.trust[tenantA]}, 30*time.Second, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := miswired.Read(ctx(t), tenantB, decision); !errors.Is(err, rowreader.ErrNotFound) {
		t.Fatalf("a login of A under B's name: %v", err)
	}
	if _, err := w.reader.Read(ctx(t), "ten_nologin", decision); !errors.Is(err, rowreader.ErrNoLogin) {
		t.Fatalf("a tenant with no login: %v", err)
	}
}

// A row of tenant B for the same correlation is never mixed into tenant A's rows: the WHERE names the tenant, and so does the login.
func TestDBAnotherTenantsRowsOfTheSameCorrelationAreNeverRead(t *testing.T) {
	w := newDBWorld(t)
	w.plant(t)
	w.intent(t, tenantB, "int_synth01", testpg.FixtureIntentHash, "syn.transaction:TX0002")
	w.decision(t, tenantB, "dec_bcase01", "evs_bcase01")
	w.grant(t, tenantB, "dec_bcase01", "grt_b001", "syn.cap.other", corr)
	got, err := w.reader.Read(ctx(t), tenantA, decision)
	if err != nil || got.SubjectRef != string(txnRef) || len(got.Executions) != 1 || got.Executions[0].GrantID != grantOne {
		t.Fatalf("%+v %v", got, err)
	}
}

// A decision with no sealed classification (the snapshot is not there) has none to show: ErrNoClassification and no rows (an early outcome has no basis, 0038).
func TestDBADecisionWithNoSealedSnapshotHasNoClassification(t *testing.T) {
	w := newDBWorld(t)
	w.decision(t, tenantA, "dec_nosnap01", "evs_nosnap01")
	got, err := w.reader.Read(ctx(t), tenantA, "dec_nosnap01")
	if !errors.Is(err, rowreader.ErrNoClassification) || got.DecisionID != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

// A decision no admitted intent of its hash backs (the intent is not there) is a mismatch: the reader refuses the whole read rather than show a classification of an intent it cannot name.
func TestDBADecisionWithNoAdmittedIntentOfItsHashIsAMismatch(t *testing.T) {
	w := newDBWorld(t)
	w.decision(t, tenantB, "dec_nointent01", "evs_nointent01")
	w.seal(t, w.evidence[tenantB], tenantB, "evs_nointent01", "EXECUTION_OUTCOME_UNKNOWN")
	if _, err := w.reader.Read(ctx(t), tenantB, "dec_nointent01"); !errors.Is(err, rowreader.ErrMismatch) {
		t.Fatalf("a decision with no admitted intent of its hash: %v", err)
	}
}

// A subject slot that is not resolved, or an intent with no such slot, has no subject: the rows are returned with an empty reference and the payment kinds become unknowns.
func TestDBAnUnresolvedSubjectIsAnEmptyReference(t *testing.T) {
	w := newDBWorld(t)
	other, err := rowreader.NewReader(rowreader.PoolsOf(w.trust), 30*time.Second, "no_such_slot")
	if err != nil {
		t.Fatal(err)
	}
	got, err := other.Read(ctx(t), tenantA, decision)
	if err != nil || got.SubjectRef != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

// The reader only reads: the Trust login it uses may write snapshots, and nothing here does (a read-only transaction), so every table holds what it held.
func TestDBTheReaderWritesNothing(t *testing.T) {
	w := newDBWorld(t)
	w.plant(t)
	count := func() string {
		var s string
		if err := w.admin.QueryRow(ctx(t), `SELECT (SELECT count(*) FROM decision_record)::text || '/' || (SELECT count(*) FROM decision_grant)::text || '/' || (SELECT count(*) FROM execution_receipt)::text || '/' ||
			(SELECT count(*) FROM outcome_verdict)::text || '/' || (SELECT count(*) FROM context_snapshot)::text || '/' || (SELECT count(*) FROM record_event)::text`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := count()
	for i := 0; i < 3; i++ {
		if _, err := w.reader.Read(ctx(t), tenantA, decision); err != nil {
			t.Fatal(err)
		}
	}
	if after := count(); after != before {
		t.Fatalf("counts %s then %s", before, after)
	}
}

// A read with no deadline is refused before the database is asked (every call has a deadline), and a bad tenant, decision or subject slot is refused at construction or at the call.
func TestDBTheReaderRefusesWhatItCannotReadSafely(t *testing.T) {
	w := newDBWorld(t)
	if _, err := w.reader.Read(context.Background(), tenantA, decision); !errors.Is(err, rowreader.ErrNoDeadline) {
		t.Errorf("no deadline: %v", err)
	}
	if _, err := w.reader.Read(ctx(t), "", decision); !errors.Is(err, rowreader.ErrNoLogin) {
		t.Errorf("no tenant: %v", err)
	}
	if _, err := w.reader.Read(ctx(t), tenantA, "dec 1; DROP"); !errors.Is(err, rowreader.ErrNotFound) {
		t.Errorf("decision shape: %v", err)
	}
	for name, args := range map[string]struct {
		pools map[string]rowreader.Beginner
		d     time.Duration
		slot  string
	}{
		"no logins":     {nil, time.Second, subjectKey},
		"nil login":     {map[string]rowreader.Beginner{tenantA: nil}, time.Second, subjectKey},
		"bad tenant id": {map[string]rowreader.Beginner{"tenant a": w.trust[tenantA]}, time.Second, subjectKey},
		"no timeout":    {rowreader.PoolsOf(w.trust), 0, subjectKey},
		"no slot":       {rowreader.PoolsOf(w.trust), time.Second, ""},
	} {
		if _, err := rowreader.NewReader(args.pools, args.d, args.slot); !errors.Is(err, rowreader.ErrConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
