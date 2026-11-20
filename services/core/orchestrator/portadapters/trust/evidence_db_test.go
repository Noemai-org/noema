package trustport_test

// A329, the Trust intake's T9 adapter over a real PostgreSQL: every migration, one trust_rw and one evidence_rw login per tenant. The database, not the adapter's code, binds
// the read to the tenant (migrations 0008, 0020, 0026: row-level security through the login's own mapping). The snapshots are written the way the sealer writes them (the
// items by the Evidence Layer's login, the snapshot by the tenant's Trust login, whose guard recomputes the hash), and the intents by the migrating login with an explicit
// admission time. A package that has several DB tests names no TestMain here: the CI database comes from NOEMA_TEST_DATABASE_URL. All data is SYNTHETIC (AGENT-25).
// Proves REQ-TRU-003, REQ-CTX-004, REQ-SEC-004, REQ-TRU-001; INV-11, INV-17.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/evidence/reconciler"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

type evdPG struct {
	admin    *pgxpool.Pool
	evidence map[string]*pgxpool.Pool
	trust    map[string]*pgxpool.Pool
}

// evdPools gives each tenant its own Trust login's pool, as the composition does: an unknown tenant has no login and no fallback.
type evdPools map[string]*pgxpool.Pool

func (p evdPools) TxFor(tenantID string) (trustport.SnapshotTx, error) {
	if pool, ok := p[tenantID]; ok {
		return pool, nil
	}
	return nil, errors.New("no login")
}

func evdNewPG(t *testing.T) *evdPG {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(evdCtx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	w := &evdPG{admin: admin, evidence: map[string]*pgxpool.Pool{}, trust: map[string]*pgxpool.Pool{}}
	for _, id := range []string{evdTenant, evdOther} {
		if _, err := admin.Exec(evdCtx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
		w.evidence[id] = testpg.TenantLogin(t, admin, dsn, "evidence_rw", id)
		w.trust[id] = testpg.TenantLogin(t, admin, dsn, "trust_rw", id)
	}
	return w
}

// intent stores one version of the case's intent chain as the migrating login, with an explicit admission time, so that the test does not depend on the server's clock.
func (w *evdPG) intent(t *testing.T, tenant string, version int, at time.Time) {
	t.Helper()
	sup := ""
	if version > 1 {
		sup = fmt.Sprintf(`,"supersedes":"int_0001@%d"`, version-1)
	}
	doc := fmt.Sprintf(`{"intent_id":"int_0001","tenant_id":%q,"correlation_id":%q,"version":%d,"status":"VALIDATED","intent_node":%q%s}`, tenant, evdCorr, version, evdNode, sup)
	var supArg any
	if version > 1 {
		supArg = fmt.Sprintf("int_0001@%d", version-1)
	}
	if _, err := w.admin.Exec(evdCtx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json, created_at)
		VALUES ($1, 'int_0001', $2, $3, $4, $5, $6, $7::jsonb, $8)`, tenant, version, supArg, evdNode, evdHash(700+version), evdCorr, doc, at); err != nil {
		t.Fatalf("intent: %v", err)
	}
}

// evdPayload is the real payload of a source, in one of three versions: 0 the usual one, 1 the same facts with other raw content (a volatile field, an unmapped field, the order of
// the entries, an entry the mapping does not look at), 2 a changed MAPPED fact (the switch timed out; the ledger's debit is no longer posted; the destination is not credited).
func evdPayload(source string, version int) map[string]any {
	posted := map[string]any{"entry_type": "DEBIT", "posting_status": "POSTED", "amount": "5000"}
	switch source {
	case evdLedger:
		switch version {
		case 1:
			return map[string]any{"entries": []any{map[string]any{"entry_type": "FEE", "posting_status": "POSTED"}, posted}, "fetched_at": "2026-11-18T11:59:21Z"}
		case 2:
			return map[string]any{"entries": []any{map[string]any{"entry_type": "DEBIT", "posting_status": "PENDING", "amount": "5000"}}}
		}
		return map[string]any{"entries": []any{posted}}
	case evdSwitch:
		switch version {
		case 1:
			return map[string]any{"semantic_status": "SUCCESS", "latency_ms": 41, "trace": "t-2"}
		case 2:
			return map[string]any{"semantic_status": "TIMED_OUT"}
		}
		return map[string]any{"semantic_status": "SUCCESS"}
	}
	switch version {
	case 1:
		return map[string]any{"credit_state": "CREDITED", "trace": "t-3"}
	case 2:
		return map[string]any{"credit_state": "NOT_CREDITED"}
	}
	return map[string]any{"credit_state": "CREDITED"}
}

// item writes one evidence item and its payload as the tenant's Evidence Layer login and returns the item as stored: the payload hash is the SHA-256 of the payload's RFC 8785 form,
// as the Evidence Layer's builder computes it (the Reconciler's payload read verifies it).
func (w *evdPG) item(t *testing.T, tenant string, it trustport.SealedItem, sla int, payload map[string]any) trustport.SealedItem {
	t.Helper()
	canon, err := crypto.CanonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canon)
	it.PayloadHash = "sha256:" + hex.EncodeToString(sum[:])
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	ref := "pay_" + tenant + "_" + it.EvidenceID
	p := w.evidence[tenant]
	if _, err := p.Exec(evdCtx(t), `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at)
		VALUES ($1, $2, $3::jsonb, $4, 'S2_internal', $5)`, ref, tenant, string(raw), evdHash(5000), it.ReadAt); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if _, err := p.Exec(evdCtx(t), `INSERT INTO evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at,
		freshness_sla_seconds, authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at)
		VALUES ($1, $2, $3, $4, $11, 'TRUST', $5, $6, $7, 'AUTHORITATIVE_FOR:debit_posted_or_reversed', 'res_synthetic', 'prn_synthetic', 'S2_internal', $8, $9, $10, $5)`,
		it.EvidenceID, tenant, it.CorrelationID, it.Source, it.ReadAt, it.EffectiveAt, sla, it.Status, it.PayloadHash, ref, it.SourceRecordID); err != nil {
		t.Fatalf("item: %v", err)
	}
	it.PayloadRef = ref
	return it
}

// seal stores the snapshot of the items as the tenant's Trust login; the database recomputes the hash and refuses a set that is not the tenant's own.
func (w *evdPG) seal(t *testing.T, tenant, snapshotID string, items []trustport.SealedItem) {
	t.Helper()
	ids, pairs := make([]string, len(items)), make([]snapshot.Pair, len(items))
	for i, it := range items {
		ids[i], pairs[i] = it.EvidenceID, snapshot.Pair{EvidenceID: it.EvidenceID, PayloadHash: it.PayloadHash}
	}
	sort.Strings(ids)
	hash, err := snapshot.SetHash(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.trust[tenant].Exec(evdCtx(t), `INSERT INTO context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at)
		VALUES ($1, $2, $3, $4, 'rec_synthetic', 'CONSISTENT', $5, $6)`, snapshotID, tenant, evdCorr, ids, hash, evdNow.Add(-30*time.Second)); err != nil {
		t.Fatalf("seal: %v", err)
	}
}

// evdCase writes the usual case for one tenant: an intent admitted five minutes before the evaluation (version 2 after a version 1 a minute earlier), the three items read
// 42 seconds before it, and the snapshot over them.
func (w *evdPG) evdCase(t *testing.T, tenant, snapshotID string) []trustport.SealedItem {
	t.Helper()
	w.intent(t, tenant, 1, evdNow.Add(-6*time.Minute))
	w.intent(t, tenant, 2, evdNow.Add(-5*time.Minute))
	var items []trustport.SealedItem
	for i, src := range []string{evdLedger, evdSwitch, evdSettle} {
		items = append(items, w.item(t, tenant, evdItem(i+1, src), 600, evdPayload(src, 0)))
	}
	w.seal(t, tenant, snapshotID, items)
	return items
}

func (w *evdPG) adapter(t *testing.T, pools evdPools) trustport.Evidence {
	t.Helper()
	r, err := trustport.NewSealedSnapshots(pools, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return evdAdapterWith(t, r, evdRules(), w.facts(t, pools))
}

// evdPayloadStore reads an evidence payload as the tenant's own Trust login, as cmd/core's trustPayloads does (that one is in package main, which a test cannot import): the database's
// row-level security, not this code, decides which tenant's rows are visible.
type evdPayloadStore struct{ pools evdPools }

func (p evdPayloadStore) Read(ctx context.Context, tenantID, ref string) ([]byte, error) {
	pool, ok := p.pools[tenantID]
	if !ok {
		return nil, errors.New("no login")
	}
	var doc []byte
	if err := pool.QueryRow(ctx, `SELECT json FROM evidence_payload WHERE tenant_id = $1 AND payload_ref = $2`, tenantID, ref).Scan(&doc); err != nil {
		return nil, errors.New("the payload read failed")
	}
	return doc, nil
}

// facts is the REAL facts source: the Reconciler of the repository's fintech pack (its rule set and its evidence mapping), reading payloads through payloadread as the tenant.
func (w *evdPG) facts(t *testing.T, pools evdPools) trustport.ItemFacts {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "..", "packs", "fintech")
	read := func(parts ...string) []byte {
		raw, err := os.ReadFile(filepath.Join(append([]string{dir}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	rs, err := reconcile.ParseRuleSet(read("recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := mapping.Parse(read("evidence_mapping", "mapping.v1.json"), rs, []string{evdLedger, evdSwitch, evdSettle, "fin.src.case_system"}, "fin.map.v1@2", mapping.Limits{MaxBytes: 1 << 20, MaxDepth: 32}, caseScope(t, filepath.Join(dir, "..", "fintech_v1.json")))
	if err != nil {
		t.Fatal(err)
	}
	r, err := reconciler.New(reconciler.Config{RuleSet: rs, Mapping: m, Payloads: payloadread.Reader{Payloads: evdPayloadStore{pools: pools}}, Clock: clock.NewManual(evdNow)})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// evdDBContentHash is the content hash of the DB case over the REAL mapping: the facts debit.state=POSTED (ledger, rec-01), switch.state=SUCCESSFUL (rec-02) and
// credit.state=CREDITED (destination, rec-03), each KNOWN, digested and then hashed with their sources, records and item statuses (KNOWN) under the domain v3 by the independent Python
// implementation (a scratch script, not committed; the same script reproduced the earlier v2 value ae27f853... with the three-element entry).
const evdDBContentHash = "sha256:9040a42674296e9bf98dab232ef20678bdc20eceeb9d186f50aa680eb36f2239"

// REQ-TRU-001, REQ-CTX-003, REQ-CTX-004; INV-17: the whole path over the real tables. The freshness is what the sealed items hold, the intent is the case's latest version.
func TestEvidenceDBReadsTheSealedSnapshotAsTheTenantsOwnLogin(t *testing.T) {
	w := evdNewPG(t)
	w.evdCase(t, evdTenant, evdSnap)
	got, err := w.adapter(t, evdPools(w.trust)).SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	if err != nil {
		t.Fatal(err)
	}
	fresh := func(id, src string) intake.SourceItem {
		return intake.SourceItem{Status: contracts.EnumsV1FactStatusKNOWN, Freshness: decide.Freshness{EvidenceID: id, EffectiveAt: evdNow.Add(-60 * time.Second).Unix(), Source: src,
			AgeSeconds: 42, EffectiveLimitSeconds: 300, LimitSetBy: decide.LimitByTrustProfile}}
	}
	var stored string // A435: EvidenceSetHash is still the snapshot's own set hash, which the database computed at the seal
	if err := w.admin.QueryRow(evdCtx(t), `SELECT evidence_set_hash FROM context_snapshot WHERE tenant_id = $1 AND snapshot_id = $2`, evdTenant, evdSnap).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	want := intake.SnapshotEvidence{IntentID: "int_0001", IntentVersion: 2, EvidenceSetHash: stored, ConfirmationBindingEvidenceHash: evdDBContentHash, Items: []intake.SourceItem{fresh("evr_01", evdLedger), fresh("evr_02", evdSwitch), fresh("evr_03", evdSettle)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// REQ-TRU-003, INV-11, REQ-SEC-004: tenant B's login cannot see tenant A's snapshot, items or intents, and asking for it is exactly the answer for an ID that does not exist. The
// check is the database's: B's pool is handed to the adapter under A's tenant ID, as a wiring mistake would, and still reads nothing.
func TestEvidenceDBAnotherTenantsSnapshotIsNotFoundForTheDatabaseItself(t *testing.T) {
	w := evdNewPG(t)
	w.evdCase(t, evdTenant, evdSnap)
	mistaken := w.adapter(t, evdPools{evdTenant: w.trust[evdOther], evdOther: w.trust[evdOther]})
	_, other := mistaken.SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	_, unknown := mistaken.SnapshotFreshness(evdCtx(t), evdTenant, "evs_ZZ99")
	if !errors.Is(other, trustport.ErrSnapshotNotFound) || !errors.Is(unknown, trustport.ErrSnapshotNotFound) || other.Error() != unknown.Error() {
		t.Errorf("another tenant's snapshot: %v; unknown snapshot: %v; want the same ErrSnapshotNotFound", other, unknown)
	}
	// Each tenant still reads its own, with its own evidence IDs, and not the other's.
	w.evdCaseB(t)
	got, err := w.adapter(t, evdPools(w.trust)).SnapshotFreshness(evdCtx(t), evdOther, "evs_BB01")
	if err != nil || len(got.Items) != 1 || got.Items[0].Freshness.EvidenceID != "evr_91" {
		t.Errorf("tenant B reads its own: %+v, %v", got, err)
	}
	if _, err := w.adapter(t, evdPools(w.trust)).SnapshotFreshness(evdCtx(t), evdOther, evdSnap); !errors.Is(err, trustport.ErrSnapshotNotFound) {
		t.Errorf("tenant B asks for tenant A's snapshot ID: err = %v, want ErrSnapshotNotFound", err)
	}
}

// evdCaseB writes a case for tenant B, a snapshot with one item of another ID.
func (w *evdPG) evdCaseB(t *testing.T) {
	t.Helper()
	w.intent(t, evdOther, 1, evdNow.Add(-5*time.Minute))
	it := w.item(t, evdOther, evdItem(91, evdLedger), 600, evdPayload(evdLedger, 0))
	w.seal(t, evdOther, "evs_BB01", []trustport.SealedItem{it})
}

// REQ-CTX-004, INV-17, EC-E16: a snapshot whose stored items were changed after the seal does not verify, and an unverifiable snapshot is an error, never evidence. The tables are
// insert-only, so the migrating login switches the trigger off to play the part of a tamperer with a database that the application cannot reach.
func TestEvidenceDBASnapshotWhoseItemsWereChangedIsNotEvidence(t *testing.T) {
	for name, tamper := range map[string]string{
		"a payload hash is changed": `UPDATE evidence_item SET payload_hash = '` + evdHash(31337) + `' WHERE evidence_id = 'evr_02'`,
		"an item is deleted":        `DELETE FROM evidence_item WHERE evidence_id = 'evr_03'`,
	} {
		t.Run(name, func(t *testing.T) {
			w := evdNewPG(t)
			w.evdCase(t, evdTenant, evdSnap)
			for _, stmt := range []string{`ALTER TABLE evidence_item DISABLE TRIGGER evidence_item_immutable`, tamper, `ALTER TABLE evidence_item ENABLE TRIGGER evidence_item_immutable`} {
				if _, err := w.admin.Exec(evdCtx(t), stmt); err != nil {
					t.Fatalf("%s: %v", stmt, err)
				}
			}
			got, err := w.adapter(t, evdPools(w.trust)).SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
			if !errors.Is(err, trustport.ErrSnapshotNotIntact) {
				t.Errorf("err = %v, want ErrSnapshotNotIntact", err)
			}
			evdNothing(t, got)
		})
	}
}

// A226: the intent of a snapshot is the case's latest version, and only when it was admitted before the evidence was read. Here version 2 is admitted after the evidence of the
// snapshot was read (the evidence was read for version 1), so the snapshot is not evidence for version 2 and the adapter does not say it is.
func TestEvidenceDBASnapshotReadForAnOlderVersionIsNotAttributedToTheNewest(t *testing.T) {
	w := evdNewPG(t)
	w.intent(t, evdTenant, 1, evdNow.Add(-6*time.Minute))
	items := []trustport.SealedItem{w.item(t, evdTenant, evdItem(1, evdLedger), 600, evdPayload(evdLedger, 0))} // read 42 seconds before the evaluation
	w.seal(t, evdTenant, evdSnap, items)
	w.intent(t, evdTenant, 2, evdNow.Add(-10*time.Second)) // admitted after the read
	got, err := w.adapter(t, evdPools(w.trust)).SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	if !errors.Is(err, trustport.ErrSnapshotIntent) {
		t.Errorf("err = %v, want ErrSnapshotIntent", err)
	}
	evdNothing(t, got)
}

// REQ-TRU-003: a tenant with no login of its own is refused, with no fallback to another tenant's pool and no tenant in the text.
func TestEvidenceDBATenantWithNoLoginIsRefused(t *testing.T) {
	w := evdNewPG(t)
	w.evdCase(t, evdTenant, evdSnap)
	_, err := w.adapter(t, evdPools{evdOther: w.trust[evdOther]}).SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	if !errors.Is(err, trustport.ErrSnapshotStore) {
		t.Errorf("err = %v, want ErrSnapshotStore", err)
	}
}

// A365 refined, INV-19, REQ-NFR-002: the facts of an item come from its stored payload, read as the tenant's Trust login. A payload that cannot be read is a real fault (the login's
// SELECT on the payload table is taken away), and the snapshot is then not evidence: the adapter fails closed, with the cause behind the fixed text.
func TestEvidenceDBAnUnreadablePayloadIsNotEvidence(t *testing.T) {
	w := evdNewPG(t)
	w.evdCase(t, evdTenant, evdSnap)
	if _, err := w.admin.Exec(evdCtx(t), `REVOKE SELECT ON evidence_payload FROM trust_rw`); err != nil {
		t.Fatal(err)
	}
	got, err := w.adapter(t, evdPools(w.trust)).SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	if !errors.Is(err, trustport.ErrSnapshotNotIntact) || !errors.Is(err, reconciler.ErrRead) {
		t.Errorf("err = %v, want ErrSnapshotNotIntact over reconciler.ErrRead", err)
	}
	evdNothing(t, got)
}

// A365 refined, REQ-CTX-001, INV-17: a stored payload that is not the one the item's hash covers (changed after the seal by someone who can switch off the trigger) is not evidence:
// the Reconciler's classification would call the row UNKNOWN, but a binding cannot rest on it, so the adapter refuses the snapshot.
func TestEvidenceDBAStoredPayloadThatIsNotTheSealedOneIsNotEvidence(t *testing.T) {
	w := evdNewPG(t)
	w.evdCase(t, evdTenant, evdSnap)
	for _, stmt := range []string{`ALTER TABLE evidence_payload DISABLE TRIGGER evidence_payload_immutable`,
		`UPDATE evidence_payload SET json = '{"semantic_status": "TIMED_OUT"}'::jsonb WHERE payload_ref = 'pay_ten_aa_evr_02'`, `ALTER TABLE evidence_payload ENABLE TRIGGER evidence_payload_immutable`} {
		if _, err := w.admin.Exec(evdCtx(t), stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	got, err := w.adapter(t, evdPools(w.trust)).SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	if !errors.Is(err, trustport.ErrSnapshotNotIntact) || !errors.Is(err, reconciler.ErrPayload) {
		t.Errorf("err = %v, want ErrSnapshotNotIntact over reconciler.ErrPayload", err)
	}
	evdNothing(t, got)
}

// reRead writes a second snapshot of the case as the evidence layer would after another read: new evidence IDs for every item, the same source and record, and the payload of the
// given version per source (see evdPayload), with a snapshot of its own.
func (w *evdPG) reRead(t *testing.T, snapshotID string, firstID int, version func(i int) int) {
	t.Helper()
	var items []trustport.SealedItem
	for i, src := range []string{evdLedger, evdSwitch, evdSettle} {
		it := evdItem(i+1, src)
		it.EvidenceID = fmt.Sprintf("evr_%02d", firstID+i)
		items = append(items, w.item(t, evdTenant, it, 600, evdPayload(src, version(i))))
	}
	w.seal(t, evdTenant, snapshotID, items)
}

// A365 refined, REQ-TRU-009, INV-25, INV-17 (the single decision per snapshot is why a re-evaluation seals a new one): a confirmation is bound to the content of snapshot S1. The
// ACCEPT triggers a fresh evaluation that reads S2, a NEW snapshot of unchanged evidence under new evidence IDs whose raw payloads DIFFER in what the mapping drops (a volatile field,
// an unmapped field, the order and the extra entries of the ledger): its content hash is S1's, so the binding is the same and the resolution is Proceed. S3 holds ONE changed mapped
// fact (the switch timed out): another content hash, so the binding changed and nothing proceeds. The set hashes of the three snapshots all differ, and so do the payload hashes of
// S1 and S2: the real mapping, the real payloads and the real database.
func TestEvidenceDBAReReadOfUnchangedFactsKeepsTheConfirmationBindingAndAChangedFactBreaksIt(t *testing.T) {
	w := evdNewPG(t)
	w.evdCase(t, evdTenant, evdSnap)
	w.reRead(t, "evs_AA02", 41, func(int) int { return 1 })
	w.reRead(t, "evs_AA03", 51, func(i int) int {
		if i == 1 {
			return 2
		}
		return 0
	})
	a := w.adapter(t, evdPools(w.trust))
	read := func(snap string) string {
		t.Helper()
		got, err := a.SnapshotFreshness(evdCtx(t), evdTenant, snap)
		if err != nil || got.ConfirmationBindingEvidenceHash == "" {
			t.Fatalf("%s: %+v, %v", snap, got, err)
		}
		return got.ConfirmationBindingEvidenceHash
	}
	h1, h2, h3 := read(evdSnap), read("evs_AA02"), read("evs_AA03")
	var sets [3]string
	for i, snap := range []string{evdSnap, "evs_AA02", "evs_AA03"} {
		if err := w.admin.QueryRow(evdCtx(t), `SELECT evidence_set_hash FROM context_snapshot WHERE tenant_id = $1 AND snapshot_id = $2`, evdTenant, snap).Scan(&sets[i]); err != nil {
			t.Fatal(err)
		}
	}
	if sets[0] == sets[1] || sets[0] == sets[2] || sets[1] == sets[2] {
		t.Fatalf("the test is void: the snapshots share a set hash %v", sets)
	}
	var payloadHashes [2]string
	for i, id := range []string{"evr_01", "evr_41"} { // the ledger's item in S1 and in S2
		if err := w.admin.QueryRow(evdCtx(t), `SELECT payload_hash FROM evidence_item WHERE tenant_id = $1 AND evidence_id = $2`, evdTenant, id).Scan(&payloadHashes[i]); err != nil {
			t.Fatal(err)
		}
	}
	if payloadHashes[0] == payloadHashes[1] {
		t.Fatalf("the test is void: the raw payloads of S1 and S2 have the same hash")
	}
	if h1 != h2 || h1 == h3 {
		t.Fatalf("content hashes: S1 %s, S2 (unchanged facts, other raw payloads) %s, S3 (one changed fact) %s", h1, h2, h3)
	}
	req := confirm.Request{ID: "cfr_01", Requirement: "SELF", RequesterID: "usr_synth01", ExpiresAt: 2000, Binding: confirm.BindingInput{PrincipalID: "usr_synth01", IntentHash: evdHash(1),
		CapabilityID: "fin.cap.create_escalation_case", TargetRef: "txn_synth0001", ParamsHash: evdHash(2), EvidenceSetHash: h1, PlanHash: evdHash(3), PolicyVersion: evdHash(4)}}
	accept := confirm.Event{RequestID: "cfr_01", ApproverID: "usr_synth01", Response: confirm.Accept, At: 1000}
	for name, tc := range map[string]struct {
		hash string
		want confirm.Resolution
	}{"unchanged facts, new snapshot and other raw payloads": {h2, confirm.Proceed}, "a changed mapped fact": {h3, confirm.BindingChanged}} {
		fresh := req.Binding
		fresh.EvidenceSetHash = tc.hash
		if got, err := confirm.Resolve(req, accept, fresh); err != nil || got != tc.want {
			t.Errorf("%s: %s, %v; want %s", name, got, err, tc.want)
		}
	}
}

// A365 refined: each mapped fact of each source moves the binding on its own, over the real database (the ledger's debit no longer posted, the switch timed out, the destination not
// credited), and the order in which the evidence layer happened to mint the IDs does not.
func TestEvidenceDBEachMappedFactMovesTheBindingOnItsOwn(t *testing.T) {
	w := evdNewPG(t)
	w.evdCase(t, evdTenant, evdSnap)
	w.reRead(t, "evs_AA02", 41, func(int) int { return 0 })
	for i := range []string{evdLedger, evdSwitch, evdSettle} {
		w.reRead(t, fmt.Sprintf("evs_AB%02d", i), 60+10*i, func(j int) int {
			if j == i {
				return 2
			}
			return 0
		})
	}
	a := w.adapter(t, evdPools(w.trust))
	read := func(snap string) string {
		t.Helper()
		got, err := a.SnapshotFreshness(evdCtx(t), evdTenant, snap)
		if err != nil {
			t.Fatalf("%s: %v", snap, err)
		}
		return got.ConfirmationBindingEvidenceHash
	}
	base := read(evdSnap)
	if read("evs_AA02") != base {
		t.Fatal("the same facts under new IDs gave another value")
	}
	for i, src := range []string{evdLedger, evdSwitch, evdSettle} {
		if read(fmt.Sprintf("evs_AB%02d", i)) == base {
			t.Errorf("a changed fact of %s did not move the binding", src)
		}
	}
}
