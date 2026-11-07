package policystore_test

// TRU-08 slice 1 (REQ-TRU-005, REQ-OPS-003; INV-09, INV-21; ruling D1): the policy versions a tenant has published, and which one is active. The active
// version of a policy_id is the highest one published for it; versions are compared only within one policy_id; a version that is not higher than the
// active one (the same, or a downgrade) is refused by the database, not by this package. Publishing writes POLICY_PUBLISHED on the Record in the same
// transaction. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const tenantA, tenantB = "ten_aa", "ten_bb"

var epoch = time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

type world struct {
	admin          *pgxpool.Pool
	trustA, trustB *pgxpool.Pool
	execA          *pgxpool.Pool
	outcomeA       *pgxpool.Pool
	store          *policystore.Store
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T')`, id); err != nil {
			t.Fatal(err)
		}
	}
	clk := clock.NewManual(epoch)
	idg := ids.New(clk, rand.New(rand.NewSource(1)))
	return &world{admin: admin,
		trustA: testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantA), trustB: testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantB),
		execA: testpg.TenantLogin(t, admin, dsn, "execution_rw", tenantA), outcomeA: testpg.TenantLogin(t, admin, dsn, "outcome_rw", tenantA),
		store: &policystore.Store{Appender: &chain.Appender{Clock: clk, IDs: idg}}}
}

func (w *world) publish(t *testing.T, p *pgxpool.Pool, tenant, policyID, version string) error {
	t.Helper()
	tx, err := p.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	err = w.store.Publish(ctx(t), tx, policystore.Request{TenantID: tenant, CorrelationID: "corr_pub0001", PolicyID: policyID, Version: version, PublishedBy: "adm_synth01"})
	if err != nil {
		_ = tx.Rollback(ctx(t))
		return err
	}
	if err := tx.Commit(ctx(t)); err != nil {
		return err
	}
	return nil
}

func active(t *testing.T, q policystore.Queryer, tenant, policyID string) string {
	t.Helper()
	v, err := policystore.Active(ctx(t), q, tenant, policyID)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The active version is the newest published, as policy_id@version, and "" when the policy has none.
func TestTheActiveVersionIsTheNewestPublishedAndEmptyWhenThereIsNone(t *testing.T) {
	w := newWorld(t)
	if v := active(t, w.execA, tenantA, "fin.pol"); v != "" {
		t.Fatalf("active %q before any publication, want none", v)
	}
	for _, v := range []string{"1.0", "1.9", "1.10", "2.0.1"} {
		if err := w.publish(t, w.trustA, tenantA, "fin.pol", v); err != nil {
			t.Fatalf("publishing %s: %v", v, err)
		}
	}
	if v := active(t, w.execA, tenantA, "fin.pol"); v != "fin.pol@2.0.1" {
		t.Fatalf("active %q, want fin.pol@2.0.1", v)
	}
}

// Versions are numbers, not text: 1.10 is newer than 1.9, and leading zeros do not change the number (ruling D1).
func TestVersionsCompareAsNumbersAndNotAsText(t *testing.T) {
	w := newWorld(t)
	for _, v := range []string{"1.9", "1.10"} {
		if err := w.publish(t, w.trustA, tenantA, "fin.pol", v); err != nil {
			t.Fatalf("publishing %s after a lower number: %v", v, err)
		}
	}
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "01.010"); !errors.Is(err, policystore.ErrNotNewer) {
		t.Fatalf("01.010 is the number 1.10 again: err %v, want ErrNotNewer", err)
	}
	if v := active(t, w.execA, tenantA, "fin.pol"); v != "fin.pol@1.10" {
		t.Fatalf("active %q, want fin.pol@1.10", v)
	}
}

// A version that is not higher than the active one is refused, by the database: the same version again, and every downgrade. The active version
// does not change, and nothing is written to the Record for a refused publication.
func TestARepublishOrADowngradeIsRefusedAndWritesNothing(t *testing.T) {
	w := newWorld(t)
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "3.1"); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"3.1", "3.0", "2.9.9", "3.1.0"} {
		if err := w.publish(t, w.trustA, tenantA, "fin.pol", v); !errors.Is(err, policystore.ErrNotNewer) {
			t.Errorf("publishing %s over 3.1: err %v, want ErrNotNewer", v, err)
		}
	}
	if v := active(t, w.execA, tenantA, "fin.pol"); v != "fin.pol@3.1" {
		t.Fatalf("active %q after refused publications, want fin.pol@3.1", v)
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("POLICY_PUBLISHED events %d (%v), want 1: a refused publication leaves no event", n, err)
	}
}

// Versions are never compared across policy IDs: a high version of one policy does not block, or stand in for, another's.
func TestVersionsAreComparedOnlyWithinOnePolicyID(t *testing.T) {
	w := newWorld(t)
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "9.0"); err != nil {
		t.Fatal(err)
	}
	if err := w.publish(t, w.trustA, tenantA, "fin.other", "1.0"); err != nil {
		t.Fatalf("1.0 of another policy after 9.0 of fin.pol: %v", err)
	}
	if a, b := active(t, w.execA, tenantA, "fin.pol"), active(t, w.execA, tenantA, "fin.other"); a != "fin.pol@9.0" || b != "fin.other@1.0" {
		t.Fatalf("active %q and %q", a, b)
	}
}

// A policy_id or version outside the grant contract's pattern is refused before anything is written.
func TestAMalformedPolicyIDOrVersionIsRefused(t *testing.T) {
	w := newWorld(t)
	for name, c := range map[string][2]string{
		"an upper-case policy id": {"Fin.pol", "1.0"}, "a version with a letter": {"fin.pol", "1.x"}, "one component": {"fin.pol", "1"},
		"four components": {"fin.pol", "1.0.0.0"}, "an empty version": {"fin.pol", ""}, "an at sign in the id": {"fin@pol", "1.0"}, "a leading dot": {"fin.pol", ".1.0"},
	} {
		if err := w.publish(t, w.trustA, tenantA, c[0], c[1]); !errors.Is(err, policystore.ErrRequest) {
			t.Errorf("%s: err %v, want ErrRequest", name, err)
		}
	}
	if v := active(t, w.execA, tenantA, "fin.pol"); v != "" {
		t.Fatalf("active %q after malformed publications", v)
	}
}

// Tenant isolation (REQ-SEC-004): each tenant has its own versions, a login sees only its tenant's, and a Trust login cannot publish for another tenant.
func TestPublicationsAreTenantScoped(t *testing.T) {
	w := newWorld(t)
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "5.0"); err != nil {
		t.Fatal(err)
	}
	if err := w.publish(t, w.trustB, tenantB, "fin.pol", "1.0"); err != nil {
		t.Fatalf("tenant B's own first version: %v", err)
	}
	if err := w.publish(t, w.trustB, tenantA, "fin.pol", "6.0"); err == nil {
		t.Fatal("tenant B's Trust login published for tenant A")
	}
	if a, b := active(t, w.execA, tenantA, "fin.pol"), active(t, w.trustB, tenantB, "fin.pol"); a != "fin.pol@5.0" || b != "fin.pol@1.0" {
		t.Fatalf("active A %q, B %q", a, b)
	}
	// a read of the other tenant's policy through one's own login finds nothing
	if v := active(t, w.execA, tenantB, "fin.pol"); v != "" {
		t.Fatalf("tenant A's Execution login read tenant B's policy %q", v)
	}
}

// Only Trust publishes. Execution reads the active version (E4); Outcome has no business with it.
func TestOnlyTrustPublishesAndExecutionReads(t *testing.T) {
	w := newWorld(t)
	if err := w.publish(t, w.execA, tenantA, "fin.pol", "1.0"); err == nil {
		t.Fatal("an Execution login published a policy version")
	}
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "1.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := policystore.Active(ctx(t), w.outcomeA, tenantA, "fin.pol"); err == nil {
		t.Fatal("an Outcome login read the policy publications")
	}
	// the table is insert-only: even Trust cannot edit or delete a publication
	for _, q := range []string{`UPDATE policy_publication SET version = '9.9'`, `DELETE FROM policy_publication`} {
		if _, err := w.trustA.Exec(ctx(t), q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
}

// A publication is on the Record: POLICY_PUBLISHED, emitted by Trust, naming who published and the version (REQ-AUD-001, INV-09).
func TestAPublicationIsOnTheRecordWithItsPublisher(t *testing.T) {
	w := newWorld(t)
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "3.1"); err != nil {
		t.Fatal(err)
	}
	var typ, by, actor, ref, payload string
	err := w.admin.QueryRow(ctx(t), `SELECT event_type, written_by, actor, object_ref, payload::text FROM record_event WHERE tenant_id = $1`, tenantA).Scan(&typ, &by, &actor, &ref, &payload)
	if err != nil {
		t.Fatal(err)
	}
	if typ != "POLICY_PUBLISHED" || by != "core-trust" || actor != "admin:adm_synth01" || ref != "pol_fin_pol_3_1" {
		t.Fatalf("event %s by %s actor %s ref %s", typ, by, actor, ref)
	}
	for _, want := range []string{`"policy_id": "fin.pol"`, `"version": "3.1"`, `"published_by": "adm_synth01"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("payload %s lacks %s", payload, want)
		}
	}
}

// The guard that refuses a downgrade is SECURITY DEFINER and reads the table as its owner, which FORCE subjects to the policies (Codex P1 on noema#168).
// A superuser owner bypasses row security, so only the catalog shows it: a SELECT policy must reach the owner of the guard function.
func TestTheGuardsOwnerCanReadThePublicationsItCompares(t *testing.T) {
	w := newWorld(t)
	var reaches, forced bool
	err := w.admin.QueryRow(ctx(t), `
		SELECT EXISTS (SELECT 1 FROM pg_policy p, pg_proc f
		                WHERE p.polrelid = 'policy_publication'::regclass AND p.polcmd IN ('r', '*')
		                  AND f.proname = 'policy_publication_guard' AND (f.proowner = ANY (p.polroles) OR p.polroles = '{0}')),
		       (SELECT relforcerowsecurity FROM pg_class WHERE oid = 'policy_publication'::regclass)`).Scan(&reaches, &forced)
	if err != nil {
		t.Fatal(err)
	}
	if !forced || !reaches {
		t.Fatalf("row security forced %v, a SELECT policy reaches the guard's owner %v; want both", forced, reaches)
	}
}
