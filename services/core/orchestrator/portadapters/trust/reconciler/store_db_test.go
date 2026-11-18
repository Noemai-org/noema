package reconcilerport_test

// TRU-07 (A329): the Store and the adapter over a real database, as the tenant's own Trust login: REQ-TRU-003, REQ-CTX-004, REQ-SEC-004; INV-11, INV-12, INV-17, INV-21.
// These need a PostgreSQL server (NOEMA_TEST_DATABASE_URL, or the binaries on the machine) with the migrations applied; CI runs them. Names start with TestDB so that a machine with no
// server can leave them out with -skip TestDB. SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	reconcilerport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/reconciler"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

const (
	dbTenantA = "ten_aa"
	dbTenantB = "ten_bb"
)

type dbWorld struct {
	*world
	admin *pgxpool.Pool
	trust map[string]*pgxpool.Pool
	ids   []string
}

// newDBWorld migrates a database and seals, as tenant A's Trust login, a snapshot over three items of tenant A that the database holds.
func newDBWorld(t *testing.T, specs ...spec) *dbWorld {
	t.Helper()
	w := newWorld(t, specs...)
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	d := &dbWorld{world: w, admin: admin, trust: map[string]*pgxpool.Pool{}}
	evidence := map[string]*pgxpool.Pool{}
	for _, id := range []string{dbTenantA, dbTenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
		evidence[id] = testpg.TenantLogin(t, admin, dsn, "evidence_rw", id)
		d.trust[id] = testpg.TenantLogin(t, admin, dsn, "trust_rw", id)
	}
	items := make([]layer.Item, len(w.sealed.Items))
	var pairs []snapshot.Pair
	for i, it := range w.sealed.Items {
		raw, err := json.Marshal(map[string]any{"synthetic": true, "n": i})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := evidence[dbTenantA].Exec(ctx(t), `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at)
			VALUES ($1, $2, $3, $4, 'S2_internal', '2026-11-17T09:00:00Z')`, it.PayloadRef, dbTenantA, string(raw), it.PayloadHash); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if _, err := evidence[dbTenantA].Exec(ctx(t), `INSERT INTO evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at,
			freshness_sla_seconds, authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at)
			VALUES ($1, $2, 'corr_synthetic', $3, 'rec-1', 'TRUST', $4, $5, $6, 'NONE', 'res_synthetic', 'prn_synthetic', 'S2_internal', $7, $8, $9, '2026-11-17T09:00:00Z')`,
			it.EvidenceID, dbTenantA, it.Source, it.ReadAt, it.EffectiveAt, it.EffectiveFreshnessSeconds, string(it.Status), it.PayloadHash, it.PayloadRef); err != nil {
			t.Fatalf("item: %v", err)
		}
		items[i] = it
		pairs = append(pairs, snapshot.Pair{EvidenceID: it.EvidenceID, PayloadHash: it.PayloadHash})
		d.ids = append(d.ids, it.EvidenceID)
	}
	hash, err := snapshot.SetHash(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.trust[dbTenantA].Exec(ctx(t), `INSERT INTO context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at)
		VALUES ($1, $2, 'corr_synthetic', $3, $4, $5, $6, $7)`, snapID, dbTenantA, d.ids, w.sealed.ReconciliationID, w.sealed.Classification, hash, w.sealed.SealedAt); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	w.sealed.TenantID, w.sealed.EvidenceSetHash = dbTenantA, hash
	return d
}

func (d *dbWorld) store(t *testing.T) *reconcilerport.Store {
	t.Helper()
	s, err := reconcilerport.NewStore(reconcilerport.PoolsOf(d.trust))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// REQ-CTX-004: what the database holds is what the store returns, to the microsecond, so that the items classify as they did when they were sealed.
func TestDBTheStoreReturnsTheSealedSnapshotAndItsItems(t *testing.T) {
	d := newDBWorld(t, agree()...)
	got, err := d.store(t).Sealed(ctx(t), dbTenantA, snapID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != dbTenantA || got.SnapshotID != snapID || got.Classification != d.sealed.Classification || got.ReconciliationID != d.sealed.ReconciliationID ||
		got.EvidenceSetHash != d.sealed.EvidenceSetHash || !got.SealedAt.Equal(d.sealed.SealedAt) || fmt.Sprint(got.EvidenceIDs) != fmt.Sprint(d.ids) {
		t.Fatalf("snapshot = %+v", got)
	}
	if len(got.Items) != len(d.sealed.Items) {
		t.Fatalf("%d items, want %d", len(got.Items), len(d.sealed.Items))
	}
	for i, it := range got.Items {
		w := d.sealed.Items[i]
		if it.EvidenceID != w.EvidenceID || it.PayloadRef != w.PayloadRef || it.PayloadHash != w.PayloadHash || it.Status != w.Status || it.Source != w.Source ||
			!it.ReadAt.Equal(w.ReadAt) || !it.EffectiveAt.Equal(w.EffectiveAt) || it.EffectiveFreshnessSeconds != w.EffectiveFreshnessSeconds {
			t.Errorf("item %d = %+v, want %+v", i, it, w)
		}
	}
}

// REQ-TRU-003, INV-11, REQ-SEC-004: tenant B's login cannot see tenant A's snapshot, with the tenant named correctly or not: the answer is the one for an id that does not exist, and a
// store wired with A's login under B's name (a wiring fault) still finds nothing for B, because the database binds the tenant, not the code.
func TestDBAnotherTenantsSnapshotIsNotFoundForTheOtherTenantsLogin(t *testing.T) {
	d := newDBWorld(t, agree()...)
	s := d.store(t)
	_, errForeign := s.Sealed(ctx(t), dbTenantB, snapID)
	_, errUnknown := s.Sealed(ctx(t), dbTenantB, "evs_nosuch01")
	if !errors.Is(errForeign, reconcilerport.ErrNotFound) || !errors.Is(errUnknown, reconcilerport.ErrNotFound) || errForeign.Error() != errUnknown.Error() {
		t.Fatalf("foreign %v, unknown %v; want the same ErrNotFound", errForeign, errUnknown)
	}
	miswired, err := reconcilerport.NewStore(map[string]reconcilerport.Beginner{dbTenantB: d.trust[dbTenantA]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := miswired.Sealed(ctx(t), dbTenantB, snapID); !errors.Is(err, reconcilerport.ErrNotFound) {
		t.Fatalf("a login of A under B's name: err = %v, want ErrNotFound", err)
	}
}

// The store only reads: the Trust login it uses may write snapshots, and nothing here does (a read-only transaction), so the table holds the one snapshot afterwards.
func TestDBTheStoreWritesNothing(t *testing.T) {
	d := newDBWorld(t, agree()...)
	if _, err := d.store(t).Sealed(ctx(t), dbTenantA, snapID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.admin.QueryRow(ctx(t), `SELECT count(*) FROM context_snapshot`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d snapshots, %v; want 1", n, err)
	}
}

// INV-21, REQ-RES-001, REQ-RES-002 end to end: the adapter over the database store and the real Reconciler returns, from the stored snapshot alone, the classification that was sealed,
// frozen when it is a contradiction; and a snapshot whose stored classification is not what its items give is a drift, not an answer.
func TestDBTheAdapterAnswersFromTheStoredSnapshotAlone(t *testing.T) {
	for name, c := range map[string]struct {
		specs  []spec
		class  reconcile.Classification
		frozen bool
	}{
		"settled":       {agree(), reconcile.SuccessConfirmed, false},
		"contradiction": {[]spec{ledgerPosted(), switchIs("REJECTED"), destIs("CREDITED")}, reconcile.ContradictoryState, true},
		"unknown":       {[]spec{ledgerPosted(), switchIs("TIMED_OUT"), destIs("PENDING")}, reconcile.ExecutionOutcomeUnknown, false},
	} {
		t.Run(name, func(t *testing.T) {
			d := newDBWorld(t, c.specs...)
			a, err := reconcilerport.New(reconcilerport.Config{Snapshots: d.store(t), Classifiers: reconcilerport.ReconcilerAt(d.baseConf), Pack: finPack(), Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			got, err := a.Reconcile(ctx(t), dbTenantA, snapID)
			if err != nil || got.Classification != c.class || got.Frozen != c.frozen {
				t.Fatalf("got %+v, %v; want %s frozen=%v", got, err, c.class, c.frozen)
			}
			if _, err := a.Reconcile(ctx(t), dbTenantB, snapID); !errors.Is(err, reconcilerport.ErrNotFound) {
				t.Fatalf("tenant B asking for A's snapshot: err = %v, want ErrNotFound", err)
			}
		})
	}
	t.Run("drift", func(t *testing.T) {
		d := newDBWorld(t, agree()...)
		other := newWorld(t, ledgerPosted(), switchIs("REJECTED"), destIs("CREDITED")) // the same three items would classify as a contradiction if their payloads were these
		for ref, raw := range other.pay.byRef {
			d.pay.byRef[ref] = raw
		}
		a, err := reconcilerport.New(reconcilerport.Config{Snapshots: d.store(t), Classifiers: reconcilerport.ReconcilerAt(d.baseConf), Pack: finPack(), Timeout: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Reconcile(ctx(t), dbTenantA, snapID); !errors.Is(err, reconcilerport.ErrDrift) {
			t.Fatalf("err = %v, want a drift (the stored payloads are no longer the sealed ones)", err)
		}
	})
}
