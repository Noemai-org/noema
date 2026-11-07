package snapshot_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

// Tests for the context_snapshot table and the evidence_verify login (EVD-03 slice 2): REQ-CTX-004 (sealed snapshots), INV-17 (no grant over an
// empty or unsealed set), INV-11 and REQ-SEC-004 (tenant isolation in the database), DEC-2026-10-16 C1, D9 (the nightly recompute login).
// All data is SYNTHETIC.

const (
	tenantA = "ten_aa"
	tenantB = "ten_bb"
	denied  = "42501"
)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

func code(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// hashN is a synthetic payload hash: 64 lowercase hex characters made from n.
func hashN(n int) string { return fmt.Sprintf("sha256:%064x", n) }

type world struct {
	admin    *pgxpool.Pool
	dsn      string
	evidence map[string]*pgxpool.Pool
	trust    map[string]*pgxpool.Pool
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	w := &world{admin: admin, dsn: dsn, evidence: map[string]*pgxpool.Pool{}, trust: map[string]*pgxpool.Pool{}}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
		w.evidence[id] = testpg.TenantLogin(t, admin, dsn, "evidence_rw", id)
		w.trust[id] = testpg.TenantLogin(t, admin, dsn, "trust_rw", id)
	}
	return w
}

// item writes one evidence item and its payload as the tenant's Evidence Layer login and returns the item's ID.
func (w *world) item(t *testing.T, tenant string, n int) string {
	return w.itemWithHash(t, tenant, n, n)
}

// itemWithHash is item with the payload hash made from hn, so that two items can share one payload hash.
func (w *world) itemWithHash(t *testing.T, tenant string, n, hn int) string {
	t.Helper()
	ref, id := fmt.Sprintf("pay_%s_%d", tenant, n), fmt.Sprintf("evr_%s%d", strings.TrimPrefix(tenant, "ten_"), n)
	p := w.evidence[tenant]
	if _, err := p.Exec(ctx(t), `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at)
		VALUES ($1, $2, '{"synthetic": true, "marker": "payload-body-must-not-leak"}', $3, 'S2_internal', '2026-11-05T09:00:00Z')`, ref, tenant, hashN(1000+n)); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if _, err := p.Exec(ctx(t), `INSERT INTO evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at,
		freshness_sla_seconds, authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at)
		VALUES ($1, $2, 'corr_synthetic', 'fin.src.core_ledger', 'rec-1', 'TRUST', '2026-11-05T09:00:00Z', '2026-11-05T08:59:00Z',
		60, 'AUTHORITATIVE_FOR:debit_posted_or_reversed', 'res_synthetic', 'prn_synthetic', 'S2_internal', 'KNOWN', $3, $4, '2026-11-05T09:00:00Z')`,
		id, tenant, hashN(hn), ref); err != nil {
		t.Fatalf("item: %v", err)
	}
	return id
}

// honest is the evidence_set_hash of the items numbered ns, as the Go function computes it.
func honest(t *testing.T, ns ...int) string {
	t.Helper()
	var pairs []snapshot.Pair
	for _, n := range ns {
		pairs = append(pairs, snapshot.Pair{EvidenceID: fmt.Sprintf("evr_aa%d", n), PayloadHash: hashN(n)})
	}
	h, err := snapshot.SetHash(pairs)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func insertSnap(p *pgxpool.Pool, c context.Context, tenant, id string, ids []string, hash string) error {
	_, err := p.Exec(c, `INSERT INTO context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at)
		VALUES ($1, $2, 'corr_synthetic', $3, 'rec_synthetic', 'CONSISTENT', $4, '2026-11-05T09:01:00Z')`, id, tenant, ids, hash)
	return err
}

func count(t *testing.T, p *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	if err := p.QueryRow(ctx(t), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// REQ-CTX-004, DEC-2026-10-16 C1: the database's own hash function is the golden vectors' construction (the same file as hash_test.go).
func TestTheSQLSetHashAgreesWithTheGoldenVectors(t *testing.T) {
	w := newWorld(t)
	raw, err := os.ReadFile("../../../../contracts/fixtures/golden_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Positive []vec      `json:"evidence_set_hash_vectors"`
		Negative []negative `json:"evidence_set_hash_negative_vectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	split := func(ps []snapshot.Pair) (ids, hashes []string) {
		ids, hashes = []string{}, []string{}
		for _, p := range ps {
			ids, hashes = append(ids, p.EvidenceID), append(hashes, p.PayloadHash)
		}
		return
	}
	for _, v := range doc.Positive {
		ids, hashes := split(v.Input)
		var got string
		if err := w.admin.QueryRow(ctx(t), `SELECT evidence_set_hash_of($1::text[], $2::text[])`, ids, hashes).Scan(&got); err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		if got != v.Expected {
			t.Errorf("%s: SQL hash %s, golden %s", v.Name, got, v.Expected)
		}
	}
	for _, n := range doc.Negative {
		ids, hashes := split(n.Input)
		var got string
		if err := w.admin.QueryRow(ctx(t), `SELECT evidence_set_hash_of($1::text[], $2::text[])`, ids, hashes).Scan(&got); err == nil {
			t.Errorf("%s: the SQL function accepted it (%s): %s", n.Name, got, n.Reason)
		}
	}
	if err := w.admin.QueryRow(ctx(t), `SELECT evidence_set_hash_of('{a,b}'::text[], '{}'::text[])`).Scan(new(string)); err == nil {
		t.Error("arrays of different lengths were accepted")
	}
}

// REQ-CTX-004, INV-17: the tenant's own Trust login stores a snapshot whose hash the database recomputed from the stored items.
func TestTheTrustLoginStoresAnHonestSnapshot(t *testing.T) {
	w := newWorld(t)
	id1, id2 := w.item(t, tenantA, 1), w.item(t, tenantA, 2)
	if err := insertSnap(w.trust[tenantA], ctx(t), tenantA, "evs_one", []string{id1, id2}, honest(t, 1, 2)); err != nil {
		t.Fatalf("an honest snapshot was refused: %v", err)
	}
	if n := count(t, w.trust[tenantA], `SELECT count(*) FROM context_snapshot`); n != 1 {
		t.Errorf("trust sees %d snapshots, want 1", n)
	}
}

// REQ-CTX-004, INV-17, INV-11: the database refuses every snapshot that is not an honest seal of the tenant's own stored evidence.
func TestTheDatabaseRefusesASnapshotThatIsNotAnHonestSeal(t *testing.T) {
	w := newWorld(t)
	a1, a2 := w.item(t, tenantA, 1), w.item(t, tenantA, 2)
	b3 := w.item(t, tenantB, 3)
	// Two items with one payload hash: a permuted id list then has the same pairs-hash, so only the order check can refuse it.
	s4, s5 := w.itemWithHash(t, tenantA, 4, 9), w.itemWithHash(t, tenantA, 5, 9)
	tie, err := snapshot.SetHash([]snapshot.Pair{{EvidenceID: s4, PayloadHash: hashN(9)}, {EvidenceID: s5, PayloadHash: hashN(9)}})
	if err != nil {
		t.Fatal(err)
	}
	good := honest(t, 1, 2)
	flipped := good[:len(good)-1] + map[bool]string{true: "1", false: "0"}[good[len(good)-1] == '0']
	empty := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	cases := []struct {
		name   string
		tenant string
		ids    []string
		hash   string
	}{
		{"a hash that does not match the items", tenantA, []string{a1, a2}, flipped},
		{"the hash of the empty set over a real set", tenantA, []string{a1, a2}, empty},
		{"an empty set with its own hash", tenantA, []string{}, empty},
		{"an empty set with any other hash", tenantA, []string{}, good},
		{"an evidence id that does not exist", tenantA, []string{a1, "evr_aa99"}, good},
		{"another tenant's evidence", tenantA, []string{a1, b3}, good},
		{"ids not sorted", tenantA, []string{a2, a1}, good},
		{"ids not sorted over equal payload hashes", tenantA, []string{s5, s4}, tie},
		{"a repeated id", tenantA, []string{a1, a1, a2}, good},
		{"a NULL id", tenantA, nil, good},
		{"a row naming another tenant", tenantB, []string{a1, a2}, good},
	}
	for i, c := range cases {
		id := fmt.Sprintf("evs_bad%d", i)
		ids := c.ids
		var err error
		if c.name == "a NULL id" {
			_, err = w.trust[tenantA].Exec(ctx(t), `INSERT INTO context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at)
				VALUES ($1, $2, 'corr_synthetic', ARRAY[$3, NULL]::text[], 'rec_synthetic', 'CONSISTENT', $4, '2026-11-05T09:01:00Z')`, id, c.tenant, a1, c.hash)
		} else {
			err = insertSnap(w.trust[tenantA], ctx(t), c.tenant, id, ids, c.hash)
		}
		if err == nil {
			t.Errorf("%s: the database stored it", c.name)
		}
	}
	if n := count(t, w.admin, `SELECT count(*) FROM context_snapshot`); n != 0 {
		t.Errorf("%d snapshots stored from refused inserts", n)
	}
	for _, bad := range []string{"", "evs_", "snap_one", "evs_x", "evs_" + strings.Repeat("a", 33)} {
		if err := insertSnap(w.trust[tenantA], ctx(t), tenantA, bad, []string{a1, a2}, good); err == nil {
			t.Errorf("snapshot_id %q was stored", bad)
		}
	}
}

// INV-11, REQ-SEC-004: only the tenant's own Trust login writes; no other power and no other tenant's login does.
func TestOnlyTheTenantsOwnTrustLoginMayWriteASnapshot(t *testing.T) {
	w := newWorld(t)
	a1 := w.item(t, tenantA, 1)
	good := honest(t, 1)
	execution := testpg.TenantLogin(t, w.admin, w.dsn, "execution_rw", tenantA)
	outcome := testpg.TenantLogin(t, w.admin, w.dsn, "outcome_rw", tenantA)
	for name, p := range map[string]*pgxpool.Pool{
		"the Evidence Layer's login":   w.evidence[tenantA],
		"the Execution login":          execution,
		"the Outcome login":            outcome,
		"another tenant's Trust login": w.trust[tenantB],
	} {
		err := insertSnap(p, ctx(t), tenantA, "evs_nope", []string{a1}, good)
		if code(err) != denied {
			t.Errorf("%s: want SQLSTATE %s, got %v", name, denied, err)
		}
	}
	if n := count(t, w.admin, `SELECT count(*) FROM context_snapshot`); n != 0 {
		t.Errorf("%d snapshots stored", n)
	}
}

// REQ-CTX-004: a sealed snapshot never changes; adding evidence creates a new snapshot. Insert-only is a trigger and a missing privilege.
func TestASealedSnapshotCannotBeChanged(t *testing.T) {
	w := newWorld(t)
	a1 := w.item(t, tenantA, 1)
	if err := insertSnap(w.trust[tenantA], ctx(t), tenantA, "evs_one", []string{a1}, honest(t, 1)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE context_snapshot SET reconciliation_classification = 'CHANGED'`,
		`UPDATE context_snapshot SET evidence_ids = '{}'`,
		`DELETE FROM context_snapshot`,
		`TRUNCATE context_snapshot`,
	} {
		if _, err := w.trust[tenantA].Exec(ctx(t), q); code(err) != denied {
			t.Errorf("trust: %s: want %s, got %v", q, denied, err)
		}
		if _, err := w.admin.Exec(ctx(t), q); err == nil || !strings.HasPrefix(code(err), "23") {
			t.Errorf("administrator: %s: want an integrity violation from the trigger, got %v", q, err)
		}
	}
	if n := count(t, w.admin, `SELECT count(*) FROM context_snapshot WHERE reconciliation_classification = 'CONSISTENT' AND evidence_ids = ARRAY[$1]::text[]`, a1); n != 1 {
		t.Error("the sealed row changed")
	}
}

// INV-11, REQ-SEC-004: reads are bound to the login's tenant, and the table owner is held to the same policy.
func TestSnapshotsAreIsolatedByTenantAndRowSecurityIsForced(t *testing.T) {
	w := newWorld(t)
	a1, b3 := w.item(t, tenantA, 1), w.item(t, tenantB, 3)
	if err := insertSnap(w.trust[tenantA], ctx(t), tenantA, "evs_aa", []string{a1}, honest(t, 1)); err != nil {
		t.Fatal(err)
	}
	hb, _ := snapshot.SetHash([]snapshot.Pair{{EvidenceID: b3, PayloadHash: hashN(3)}})
	if err := insertSnap(w.trust[tenantB], ctx(t), tenantB, "evs_bb", []string{b3}, hb); err != nil {
		t.Fatal(err)
	}
	if n := count(t, w.trust[tenantA], `SELECT count(*) FROM context_snapshot`); n != 1 {
		t.Errorf("tenant A's Trust login sees %d snapshots, want its own 1", n)
	}
	if n := count(t, w.trust[tenantA], `SELECT count(*) FROM context_snapshot WHERE tenant_id = $1`, tenantB); n != 0 {
		t.Errorf("tenant A's Trust login sees %d of tenant B's snapshots", n)
	}
	for _, tbl := range []string{"context_snapshot", "evidence_item"} {
		var on, forced bool
		if err := w.admin.QueryRow(ctx(t), `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`, tbl).Scan(&on, &forced); err != nil {
			t.Fatal(err)
		}
		if !on || !forced {
			t.Errorf("%s: row security enabled=%v forced=%v", tbl, on, forced)
		}
	}
}

// D9: the nightly recompute's login reads both tables across tenants, and can do nothing else.
func TestEvidenceVerifyReadsTheTwoTablesAcrossTenantsAndNothingElse(t *testing.T) {
	w := newWorld(t)
	a1, b3 := w.item(t, tenantA, 1), w.item(t, tenantB, 3)
	if err := insertSnap(w.trust[tenantA], ctx(t), tenantA, "evs_aa", []string{a1}, honest(t, 1)); err != nil {
		t.Fatal(err)
	}
	hb, _ := snapshot.SetHash([]snapshot.Pair{{EvidenceID: b3, PayloadHash: hashN(3)}})
	if err := insertSnap(w.trust[tenantB], ctx(t), tenantB, "evs_bb", []string{b3}, hb); err != nil {
		t.Fatal(err)
	}
	v := testpg.EvidenceVerifyLogin(t, w.dsn)
	if n := count(t, v, `SELECT count(*) FROM context_snapshot`); n != 2 {
		t.Errorf("evidence_verify sees %d snapshots, want both tenants' 2", n)
	}
	if n := count(t, v, `SELECT count(*) FROM evidence_item`); n != 2 {
		t.Errorf("evidence_verify sees %d items, want both tenants' 2", n)
	}
	refused := []string{
		`INSERT INTO context_snapshot SELECT * FROM context_snapshot`,
		`UPDATE context_snapshot SET reconciliation_classification = 'X'`,
		`DELETE FROM context_snapshot`,
		`TRUNCATE context_snapshot`,
		`INSERT INTO evidence_item SELECT * FROM evidence_item`,
		`UPDATE evidence_item SET fact_status = 'UNKNOWN'`,
		`DELETE FROM evidence_item`,
		`TRUNCATE evidence_item`,
		`SELECT json FROM evidence_payload`,
		`SELECT payload_ref FROM evidence_item JOIN evidence_payload USING (payload_ref)`,
		`SELECT * FROM record_event`,
		`SELECT * FROM record_chain_head`,
		`SELECT * FROM tenant_login`,
		`SELECT * FROM tenant`,
		`SELECT * FROM decision_grant`,
		`SELECT * FROM grant_spend`,
		`SELECT * FROM outbox_event`,
		`SELECT * FROM outcome_verdict`,
		`SET ROLE trust_rw`,
		`SET ROLE evidence_rw`,
		`SET ROLE record_verify`,
		`SELECT evidence_login_tenant()`,
		`SELECT * FROM record_read_chain_t()`,
		`SELECT evidence_set_hash_of('{a}'::text[], '{}'::text[])`,
		`CREATE TABLE evidence_verify_scratch (x int)`,
	}
	for _, q := range refused {
		if _, err := v.Exec(ctx(t), q); code(err) != denied {
			t.Errorf("evidence_verify: %s: want SQLSTATE %s, got %v", q, denied, err)
		}
	}
	if n := count(t, w.admin, `SELECT count(*) FROM context_snapshot`); n != 2 {
		t.Errorf("%d snapshots after the refused statements", n)
	}
}

// D9: the grants of evidence_verify, read from the catalog, are exactly SELECT on two tables, with no membership, no attribute and no function.
func TestEvidenceVerifyHoldsExactlyTheGrantsD9Names(t *testing.T) {
	w := newWorld(t)
	var login, super, createdb, createrole, repl, bypass bool
	var members int
	if err := w.admin.QueryRow(ctx(t), `SELECT r.rolcanlogin, r.rolsuper, r.rolcreatedb, r.rolcreaterole, r.rolreplication, r.rolbypassrls,
		(SELECT count(*) FROM pg_auth_members m WHERE m.member = r.oid) FROM pg_roles r WHERE r.rolname = 'evidence_verify'`).Scan(&login, &super, &createdb, &createrole, &repl, &bypass, &members); err != nil {
		t.Fatal(err)
	}
	if !login || super || createdb || createrole || repl || bypass || members != 0 {
		t.Errorf("evidence_verify: login=%v super=%v createdb=%v createrole=%v replication=%v bypassrls=%v memberships=%d", login, super, createdb, createrole, repl, bypass, members)
	}
	rows, err := w.admin.Query(ctx(t), `SELECT c.relname, a.privilege_type
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace,
		LATERAL aclexplode(c.relacl) a JOIN pg_roles g ON g.oid = a.grantee
		WHERE n.nspname = 'public' AND g.rolname = 'evidence_verify' ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var rel, priv string
		if err := rows.Scan(&rel, &priv); err != nil {
			t.Fatal(err)
		}
		got = append(got, rel+":"+priv)
	}
	if want := "context_snapshot:SELECT,evidence_item:SELECT"; strings.Join(got, ",") != want {
		t.Errorf("evidence_verify table privileges = %v, want %s", got, want)
	}
	var cols int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid, LATERAL aclexplode(a.attacl) x JOIN pg_roles g ON g.oid = x.grantee
		WHERE g.rolname = 'evidence_verify'`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if cols != 0 {
		t.Errorf("evidence_verify holds %d column-level grants", cols)
	}
	var fns int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'public' AND p.prosecdef AND p.prorettype <> 'trigger'::regtype AND has_function_privilege('evidence_verify', p.oid, 'EXECUTE')`).Scan(&fns); err != nil {
		t.Fatal(err)
	}
	if fns != 0 {
		t.Errorf("evidence_verify can execute %d SECURITY DEFINER functions that are not trigger functions (a trigger function cannot be called)", fns)
	}
	var create bool
	if err := w.admin.QueryRow(ctx(t), `SELECT has_schema_privilege('evidence_verify', 'public', 'CREATE')`).Scan(&create); err != nil {
		t.Fatal(err)
	}
	if create {
		t.Error("evidence_verify may create objects in the public schema")
	}
	pol, err := w.admin.Query(ctx(t), `SELECT tablename, cmd, roles::text, qual FROM pg_policies WHERE 'evidence_verify' = ANY (roles) ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	defer pol.Close()
	var policies []string
	for pol.Next() {
		var tbl, cmd, roles, qual string
		if err := pol.Scan(&tbl, &cmd, &roles, &qual); err != nil {
			t.Fatal(err)
		}
		policies = append(policies, fmt.Sprintf("%s/%s/%s/%s", tbl, cmd, roles, qual))
	}
	if want := "context_snapshot/SELECT/{evidence_verify}/true,evidence_item/SELECT/{evidence_verify}/true"; strings.Join(policies, ",") != want {
		t.Errorf("policies naming evidence_verify = %v, want %s", policies, want)
	}
}
