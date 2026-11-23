package reconcilerport_test

// TRU-07 (DEBT-52, ruling A350): the two readers of one sealed snapshot agree. The Reconciler adapter's Store (this package, noema#739) and the Evidence adapter's SealedSnapshots
// (package trustport, noema#740) each read context_snapshot and evidence_item on their own. Until one shared reader exists, this test is what stops them from drifting: it seals one
// snapshot over several items in a real database, reads it once through each reader as the tenant's own Trust login, and asserts that they return the same snapshot and the same item
// set, field by field, to the microsecond. It changes no behaviour and adds no reader. The test lives here, as an external test package, because production code of one adapter may not
// import the other, and a test may. Every row is SYNTHETIC (AGENT-25).
// Proves REQ-CTX-004, REQ-TRU-003, REQ-SEC-004; INV-11, INV-17, INV-21.
//
// What the readers do not share, and so what this test cannot compare: the Evidence reader does not read payload_ref, sealed_at, reconciliation_id or the classification (it needs
// none of them); the Reconciler's Store does not read the item's correlation_id or the intent. Each of those is asserted against the seeded row for the reader that has it.
// These need a PostgreSQL server (NOEMA_TEST_DATABASE_URL, or the binaries on the machine); names start with TestDB so that a machine with no server can leave them out with -skip TestDB.

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	reconcilerport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/reconciler"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

const (
	rdSnap   = "evs_RD01"
	rdCorr   = "corr_RD11"
	rdNode   = "fin.payments.exception.investigate"
	rdSealed = "rec_rdsynth"
)

// rdBase is the SYNTHETIC instant the seeded rows are placed around. The times carry microseconds, so that a reader that rounds or truncates a time is seen.
var rdBase = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// rdRow is one seeded evidence item. Every field of every row differs from the others, so that a reader that swaps two columns, or reads one from the wrong row, is seen.
type rdRow struct {
	id, corr, source, status, ref, hash string
	read, effective                     time.Time
	sla                                 int64
}

// rdRows are the seeded items, in the order they are inserted: DESCENDING by ID bytes, and with IDs that sort differently by byte and by a locale (upper case before lower case
// by byte), so that a reader that leaves the database's own order, or sorts with something other than the bytes, is seen. The statuses are five of the contract's enum.
func rdRows() []rdRow {
	mk := func(n int, id, source, status string, sla int64) rdRow {
		return rdRow{id: id, corr: fmt.Sprintf("corr_RDI%d", n), source: source, status: status, ref: fmt.Sprintf("pay_rd_%d", n),
			hash:      fmt.Sprintf("sha256:%064x", 0xa000+n*17),
			read:      rdBase.Add(-time.Duration(40+n) * time.Second).Add(time.Duration(123456+n*1111) * time.Microsecond),
			effective: rdBase.Add(-time.Duration(90+n*7) * time.Second).Add(time.Duration(654321-n*999) * time.Microsecond), sla: sla}
	}
	return []rdRow{
		mk(5, "evr_c5", "fin.src.settlement", "UNAVAILABLE", 86400),
		mk(4, "evr_a4", "fin.src.core_ledger", "AMBIGUOUS", 3600),
		mk(3, "evr_B3", "fin.src.switch", "INFERRED", 600),
		mk(2, "evr_9", "fin.src.core_ledger", "STALE", 61),
		mk(1, "evr_10", "fin.src.switch", "KNOWN", 7),
	}
}

// rdIDs are the seeded IDs sorted ascending by bytes, as the sealer stores them.
func rdIDs(rows []rdRow) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.id
	}
	sort.Strings(ids)
	return ids
}

type rdWorld struct {
	admin  *pgxpool.Pool
	dsn    string
	trust  map[string]*pgxpool.Pool
	rows   []rdRow
	hash   string
	sealed time.Time
}

// rdLogins is the Evidence reader's login source over the per-tenant Trust pools: a tenant with no pool has no login.
type rdLogins map[string]*pgxpool.Pool

func (l rdLogins) TxFor(tenantID string) (trustport.SnapshotTx, error) {
	if p, ok := l[tenantID]; ok {
		return p, nil
	}
	return nil, errors.New("no login")
}

// rdSeed migrates a database, gives two tenants their Evidence and Trust logins, and seals for tenant A the snapshot over the rows (the items by the tenant's Evidence login, the snapshot
// by its Trust login, whose guard recomputes the hash) with one admitted intent of the correlation. Tenant B holds a snapshot of its own under another ID.
func rdSeed(t *testing.T) *rdWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	w := &rdWorld{admin: admin, dsn: dsn, trust: map[string]*pgxpool.Pool{}, rows: rdRows(), sealed: rdBase.Add(-30*time.Second + 987654*time.Microsecond)}
	evidence := map[string]*pgxpool.Pool{}
	for _, id := range []string{dbTenantA, dbTenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
		evidence[id] = testpg.TenantLogin(t, admin, dsn, "evidence_rw", id)
		w.trust[id] = testpg.TenantLogin(t, admin, dsn, "trust_rw", id)
	}
	w.hash = w.seal(t, evidence[dbTenantA], dbTenantA, rdSnap, w.rows)
	other := []rdRow{{id: "evr_other", corr: "corr_RDB1", source: "fin.src.switch", status: "KNOWN", ref: "pay_rd_other", hash: fmt.Sprintf("sha256:%064x", 0xbeef),
		read: rdBase.Add(-time.Minute), effective: rdBase.Add(-2 * time.Minute), sla: 300}}
	w.seal(t, evidence[dbTenantB], dbTenantB, "evs_RDB1", other)
	doc := fmt.Sprintf(`{"intent_id":"int_0001","tenant_id":%q,"correlation_id":%q,"version":1,"status":"VALIDATED","intent_node":%q}`, dbTenantA, rdCorr, rdNode)
	if _, err := admin.Exec(ctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json, created_at)
		VALUES ($1, 'int_0001', 1, NULL, $2, $3, $4, $5::jsonb, $6)`, dbTenantA, rdNode, fmt.Sprintf("sha256:%064x", 0x1717), rdCorr, doc, rdBase.Add(-6*time.Minute)); err != nil {
		t.Fatalf("intent: %v", err)
	}
	// Tenant B holds a longer chain for the SAME correlation ID, with other nodes: it is not tenant A's intent, whoever reads it.
	for v := 1; v <= 2; v++ {
		sup, supArg := "", any(nil)
		if v > 1 {
			sup, supArg = fmt.Sprintf(`,"supersedes":"int_0001@%d"`, v-1), fmt.Sprintf("int_0001@%d", v-1)
		}
		node := fmt.Sprintf("fin.payments.exception.other%d", v)
		doc := fmt.Sprintf(`{"intent_id":"int_0001","tenant_id":%q,"correlation_id":%q,"version":%d,"status":"VALIDATED","intent_node":%q%s}`, dbTenantB, rdCorr, v, node, sup)
		if _, err := admin.Exec(ctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json, created_at)
			VALUES ($1, 'int_0001', $2, $3, $4, $5, $6, $7::jsonb, $8)`, dbTenantB, v, supArg, node, fmt.Sprintf("sha256:%064x", 0x2727+v), rdCorr, doc, rdBase.Add(-time.Duration(5-v)*time.Minute)); err != nil {
			t.Fatalf("tenant B intent: %v", err)
		}
	}
	return w
}

// seal inserts the rows as the tenant's Evidence login, in the order given, and the snapshot over them as its Trust login; it returns the set hash the sealer computes.
func (w *rdWorld) seal(t *testing.T, evidence *pgxpool.Pool, tenant, snapshotID string, rows []rdRow) string {
	t.Helper()
	pairs := make([]snapshot.Pair, len(rows))
	for i, r := range rows {
		if _, err := evidence.Exec(ctx(t), `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at)
			VALUES ($1, $2, '{"synthetic": true}', $3, 'S2_internal', $4)`, r.ref, tenant, r.hash, r.read); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if _, err := evidence.Exec(ctx(t), `INSERT INTO evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at,
			freshness_sla_seconds, authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at)
			VALUES ($1, $2, $3, $4, 'rec-1', 'TRUST', $5, $6, $7, 'NONE', 'res_synthetic', 'prn_synthetic', 'S2_internal', $8, $9, $10, $5)`,
			r.id, tenant, r.corr, r.source, r.read, r.effective, r.sla, r.status, r.hash, r.ref); err != nil {
			t.Fatalf("item: %v", err)
		}
		pairs[i] = snapshot.Pair{EvidenceID: r.id, PayloadHash: r.hash}
	}
	hash, err := snapshot.SetHash(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.trust[tenant].Exec(ctx(t), `INSERT INTO context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at)
		VALUES ($1, $2, $3, $4, $5, 'CONSISTENT', $6, $7)`, snapshotID, tenant, rdCorr, rdIDs(rows), rdSealed, hash, w.sealed); err != nil {
		t.Fatalf("seal: %v", err)
	}
	return hash
}

// readers builds both readers over the given pools, one per tenant, exactly as cmd/core wires them.
func (w *rdWorld) readers(t *testing.T, pools map[string]*pgxpool.Pool) (*reconcilerport.Store, trustport.SealedSnapshots) {
	t.Helper()
	store, err := reconcilerport.NewStore(reconcilerport.PoolsOf(pools))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := trustport.NewSealedSnapshots(rdLogins(pools), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return store, sealed
}

// rdItem is what both readers say about an item, in the types they share: the times as microseconds since the epoch (an instant, not a location), the status as text.
type rdItem struct {
	ID, Source, Status, PayloadHash string
	ReadAtMicro, EffectiveAtMicro   int64
	FreshnessSeconds                int64
}

func rdWant(rows []rdRow) []rdItem {
	out := make([]rdItem, len(rows))
	for i, r := range rows {
		out[i] = rdItem{r.id, r.source, r.status, r.hash, r.read.UnixMicro(), r.effective.UnixMicro(), r.sla}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// REQ-CTX-004, INV-17, INV-21: one sealed snapshot, read through both readers, gives the same items, the same hash and the same IDs.
func TestDBTheTwoSealedSnapshotReadersAgreeOnOneSnapshot(t *testing.T) {
	w := rdSeed(t)
	store, evd := w.readers(t, w.trust)
	rec, err := store.Sealed(ctx(t), dbTenantA, rdSnap)
	if err != nil {
		t.Fatalf("the Reconciler's store: %v", err)
	}
	ev, err := evd.Sealed(ctx(t), dbTenantA, rdSnap)
	if err != nil {
		t.Fatalf("the Evidence reader: %v", err)
	}

	// The snapshot itself.
	if rec.TenantID != ev.TenantID || rec.SnapshotID != ev.SnapshotID || rec.TenantID != dbTenantA || rec.SnapshotID != rdSnap {
		t.Errorf("snapshot keys: Reconciler %q/%q, Evidence %q/%q, want %q/%q", rec.TenantID, rec.SnapshotID, ev.TenantID, ev.SnapshotID, dbTenantA, rdSnap)
	}
	if rec.EvidenceSetHash != ev.EvidenceSetHash || rec.EvidenceSetHash != w.hash {
		t.Errorf("evidence set hash: Reconciler %q, Evidence %q, sealed %q", rec.EvidenceSetHash, ev.EvidenceSetHash, w.hash)
	}
	if want := rdIDs(w.rows); !reflect.DeepEqual(rec.EvidenceIDs, ev.EvidenceIDs) || !reflect.DeepEqual(rec.EvidenceIDs, want) {
		t.Errorf("evidence IDs: Reconciler %v, Evidence %v, sealed %v", rec.EvidenceIDs, ev.EvidenceIDs, want)
	}

	// The items, field by field. The Evidence reader leaves the database's order alone (its adapter sorts), so both are put in ID order before they are compared.
	var fromRec, fromEv []rdItem
	for _, it := range rec.Items {
		fromRec = append(fromRec, rdItem{it.EvidenceID, it.Source, string(it.Status), it.PayloadHash, it.ReadAt.UnixMicro(), it.EffectiveAt.UnixMicro(), it.EffectiveFreshnessSeconds})
	}
	for _, it := range ev.Items {
		fromEv = append(fromEv, rdItem{it.EvidenceID, it.Source, it.Status, it.PayloadHash, it.ReadAt.UnixMicro(), it.EffectiveAt.UnixMicro(), it.SLASeconds})
	}
	sort.Slice(fromEv, func(i, j int) bool { return fromEv[i].ID < fromEv[j].ID })
	want := rdWant(w.rows)
	if !reflect.DeepEqual(fromRec, fromEv) {
		t.Errorf("the readers disagree on the items:\n Reconciler %+v\n Evidence   %+v", fromRec, fromEv)
	}
	if !reflect.DeepEqual(fromRec, want) || !reflect.DeepEqual(fromEv, want) {
		t.Errorf("the readers agree but not with the rows that were sealed:\n Reconciler %+v\n Evidence   %+v\n sealed     %+v", fromRec, fromEv, want)
	}
}

// What only one reader reads is held to the seeded row, so that a change to either reader's own columns is seen too (sealed_at, the reconciliation, payload_ref: the Reconciler's;
// the item's correlation and the intent: the Evidence reader's).
func TestDBEachSealedSnapshotReaderReturnsItsOwnColumnsAsSealed(t *testing.T) {
	w := rdSeed(t)
	store, evd := w.readers(t, w.trust)
	rec, err := store.Sealed(ctx(t), dbTenantA, rdSnap)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ReconciliationID != rdSealed || rec.Classification != "CONSISTENT" || rec.SealedAt.UnixMicro() != w.sealed.UnixMicro() || rec.SealedAt.Location() != time.UTC {
		t.Errorf("Reconciler snapshot columns: %q %q %v (%v), want %q CONSISTENT %v UTC", rec.ReconciliationID, rec.Classification, rec.SealedAt, rec.SealedAt.Location(), rdSealed, w.sealed)
	}
	refs := map[string]string{}
	for _, r := range w.rows {
		refs[r.id] = r.ref
	}
	if len(rec.Items) != len(w.rows) {
		t.Fatalf("Reconciler: %d items, want %d", len(rec.Items), len(w.rows))
	}
	for i, it := range rec.Items {
		if it.PayloadRef != refs[it.EvidenceID] || it.ReadAt.Location() != time.UTC || it.EffectiveAt.Location() != time.UTC {
			t.Errorf("Reconciler item %q: payload_ref %q (want %q), times in %v and %v (want UTC)", it.EvidenceID, it.PayloadRef, refs[it.EvidenceID], it.ReadAt.Location(), it.EffectiveAt.Location())
		}
		if want := rdIDs(w.rows)[i]; it.EvidenceID != want { // the Reconciler's promise: bytewise by ID, as the sealer sorts, whatever the database's collation or physical order
			t.Errorf("Reconciler item %d is %q, want %q (sorted ascending by bytes)", i, it.EvidenceID, want)
		}
	}
	ev, err := evd.Sealed(ctx(t), dbTenantA, rdSnap)
	if err != nil {
		t.Fatal(err)
	}
	corr := map[string]string{}
	for _, r := range w.rows {
		corr[r.id] = r.corr
	}
	if ev.CorrelationID != rdCorr || len(ev.Items) != len(w.rows) {
		t.Fatalf("Evidence snapshot: correlation %q with %d items, want %q with %d", ev.CorrelationID, len(ev.Items), rdCorr, len(w.rows))
	}
	for _, it := range ev.Items {
		if it.CorrelationID != corr[it.EvidenceID] {
			t.Errorf("Evidence item %q: correlation %q, want %q", it.EvidenceID, it.CorrelationID, corr[it.EvidenceID])
		}
	}
	if got, want := ev.Intent, (trustport.SealedIntent{IntentID: "int_0001", Version: 1, IntentNode: rdNode, AdmittedAt: rdBase.Add(-6 * time.Minute)}); got.IntentID != want.IntentID ||
		got.Version != want.Version || got.IntentNode != want.IntentNode || !got.AdmittedAt.Equal(want.AdmittedAt) {
		t.Errorf("Evidence intent = %+v, want %+v", got, want)
	}
}

// REQ-TRU-003, REQ-SEC-004, INV-11: a snapshot of another tenant is not found by either reader, and the answer is the one each gives for an ID that does not exist: the sentinel, with
// its own fixed text and nothing of the tenant, the snapshot or the row. The database's row-level security answers it here (tenant B's own login, then tenant B's login wired under
// tenant A's name, as a wiring mistake would).
func TestDBTheTwoSealedSnapshotReadersDoNotFindAnotherTenantsSnapshot(t *testing.T) {
	w := rdSeed(t)
	for name, c := range map[string]struct {
		pools map[string]*pgxpool.Pool
		asked string
	}{
		"tenant B asks with its own login":                {w.trust, dbTenantB},
		"tenant B's login is wired under tenant A's name": {map[string]*pgxpool.Pool{dbTenantA: w.trust[dbTenantB], dbTenantB: w.trust[dbTenantB]}, dbTenantA},
	} {
		t.Run(name, func(t *testing.T) {
			store, evd := w.readers(t, c.pools)
			w.notFound(t, "Reconciler", c.asked, func(snap string) error { _, err := store.Sealed(ctx(t), c.asked, snap); return err }, reconcilerport.ErrNotFound)
			w.notFound(t, "Evidence", c.asked, func(snap string) error { _, err := evd.Sealed(ctx(t), c.asked, snap); return err }, trustport.ErrSnapshotNotFound)
		})
	}
}

// notFound asks for tenant A's snapshot and for an ID nobody has, and wants the same sentinel and the same text for both.
func (w *rdWorld) notFound(t *testing.T, reader, asked string, read func(snapshotID string) error, sentinel error) {
	t.Helper()
	other, unknown := read(rdSnap), read("evs_RDZZ99")
	if !errors.Is(other, sentinel) || !errors.Is(unknown, sentinel) || other.Error() != unknown.Error() || other.Error() != sentinel.Error() {
		t.Errorf("%s reader, %s asking for tenant A's snapshot: %v; for an unknown one: %v; want the sentinel %q for both", reader, asked, other, unknown, sentinel)
	}
}

// REQ-SEC-004, INV-11: the readers' queries name the tenant themselves, and do not lean on row-level security alone. The nightly recompute's login (evidence_verify) reads every
// tenant's snapshots and items, and, with the grant and policy this test gives it, every tenant's intents, so with it a reader's own predicate is the only thing between tenant B's
// question and tenant A's snapshot, between a snapshot of B and the items of A, and between tenant A's correlation and tenant B's intents of the same correlation ID. The items case
// needs a snapshot that no honest writer can make (a snapshot of B that names A's items), so the migrating login switches the seal's guard off to play a forger. The Reconciler's
// snapshot query is also held by a second lock in code (the row's tenant and ID are compared with the ones asked), so only the two together are tested for that one query.
func TestDBTheTwoSealedSnapshotReadersNameTheTenantInTheirOwnQueries(t *testing.T) {
	w := rdSeed(t)
	wide := testpg.EvidenceVerifyLogin(t, w.dsn)
	// The Evidence reader's third query reads admitted_intent. The nightly login holds no grant on it, so, for this test only, the migrating login gives it a grant and a policy that
	// shows every row: it stands in for a login that reads every tenant's intents.
	for _, stmt := range []string{`GRANT SELECT ON admitted_intent TO evidence_verify`, `CREATE POLICY rd_all_intents ON admitted_intent FOR SELECT TO evidence_verify USING (true)`} {
		if _, err := w.admin.Exec(ctx(t), stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	store, evd := w.readers(t, map[string]*pgxpool.Pool{dbTenantA: wide, dbTenantB: wide})
	w.notFound(t, "Reconciler", dbTenantB, func(snap string) error { _, err := store.Sealed(ctx(t), dbTenantB, snap); return err }, reconcilerport.ErrNotFound)
	w.notFound(t, "Evidence", dbTenantB, func(snap string) error { _, err := evd.Sealed(ctx(t), dbTenantB, snap); return err }, trustport.ErrSnapshotNotFound)

	// The login sees both tenants' intents of this correlation ID; the intent of tenant A's snapshot is tenant A's own.
	own, err := evd.Sealed(ctx(t), dbTenantA, rdSnap)
	if err != nil || own.Intent.IntentNode != rdNode || own.Intent.Version != 1 || len(own.Items) != len(w.rows) {
		t.Errorf("Evidence reader, as the wide login for tenant A: intent %q v%d with %d items (err %v), want %q v1 with %d", own.Intent.IntentNode, own.Intent.Version, len(own.Items), err, rdNode, len(w.rows))
	}
	hash, err := snapshot.SetHash([]snapshot.Pair{{EvidenceID: w.rows[0].id, PayloadHash: w.rows[0].hash}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE context_snapshot DISABLE TRIGGER context_snapshot_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at)
		VALUES ('evs_RDFORGE', $1, $2, $3, $4, 'CONSISTENT', $5, $6)`, dbTenantB, rdCorr, []string{w.rows[0].id}, rdSealed, hash, w.sealed); err != nil {
		t.Fatalf("forged snapshot: %v", err)
	}
	rec, err := store.Sealed(ctx(t), dbTenantB, "evs_RDFORGE")
	if err != nil || len(rec.Items) != 0 {
		t.Errorf("Reconciler: %d items of another tenant read for tenant B's snapshot (err %v), want none", len(rec.Items), err)
	}
	ev, err := evd.Sealed(ctx(t), dbTenantB, "evs_RDFORGE")
	if err != nil || len(ev.Items) != 0 {
		t.Errorf("Evidence: %d items of another tenant read for tenant B's snapshot (err %v), want none", len(ev.Items), err)
	}
}
