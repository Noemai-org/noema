package layer_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

// Tests for migration 0008 (EVD-01 slice 2a): evidence_item and evidence_payload, PERD 38.2. REQ-CTX-001, INV-10, INV-19, INV-11.
// All data is SYNTHETIC.

const (
	tenantA   = "ten_aa"
	denied    = "42501"
	violation = "23" // an integrity constraint or an immutability trigger (class 23)
	hashA     = "sha256:aaaa111122223333aaaa111122223333aaaa111122223333aaaa111122223333"
)

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

type world struct {
	admin *pgxpool.Pool
	dsn   string
	ev    map[string]*pgxpool.Pool // the Evidence Layer's login per tenant, made on first use
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, id := range []string{tenantA, "ten_bb"} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
	}
	return &world{admin: admin, dsn: dsn, ev: map[string]*pgxpool.Pool{}}
}

func (w *world) as(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	p, err := db.OpenAsRole(ctx(t), w.dsn, role)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// evidence is the Evidence Layer's own login for one tenant: it holds evidence_rw and is mapped to the tenant (DEBT-04 option A).
func (w *world) evidence(t *testing.T, tenant string) *pgxpool.Pool {
	t.Helper()
	if w.ev[tenant] == nil {
		w.ev[tenant] = testpg.TenantLogin(t, w.admin, w.dsn, "evidence_rw", tenant)
	}
	return w.ev[tenant]
}

func insertPayload(p *pgxpool.Pool, c context.Context, tenant, ref string) error {
	_, err := p.Exec(c, `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at)
		VALUES ($1, $2, '{"synthetic": true}', $3, 'S2_internal', '2026-11-05T09:00:00Z')`, ref, tenant, hashA)
	return err
}

func insertItem(p *pgxpool.Pool, c context.Context, tenant, id, ref, status, hash string, freshness int) error {
	_, err := p.Exec(c, `INSERT INTO evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at,
		freshness_sla_seconds, authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at)
		VALUES ($1, $2, 'cor_synthetic', 'fin.src.core_ledger', 'rec-1', 'TRUST', '2026-11-05T09:00:00Z', '2026-11-05T08:59:00Z',
		$6, 'AUTHORITATIVE_FOR:debit_posted_or_reversed', 'res_synthetic', 'prn_synthetic', 'S2_internal', $4, $5, $3, '2026-11-05T09:00:00Z')`,
		id, tenant, ref, status, hash, freshness)
	return err
}

// REQ-CTX-001: the Evidence Layer persists a payload and then the item that points at it.
func TestEvidenceRwStoresAPayloadThenItsItem(t *testing.T) {
	w := newWorld(t)
	ev := w.evidence(t, tenantA)
	if err := insertPayload(ev, ctx(t), tenantA, "pay_1"); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if err := insertItem(ev, ctx(t), tenantA, "evd_1", "pay_1", "KNOWN", hashA, 300); err != nil {
		t.Fatalf("item: %v", err)
	}
}

// REQ-CTX-001: no payload and no item is mutable after insert (a database trigger, not a convention).
func TestEvidenceRowsAreImmutable(t *testing.T) {
	w := newWorld(t)
	ev := w.evidence(t, tenantA)
	if err := insertPayload(ev, ctx(t), tenantA, "pay_1"); err != nil {
		t.Fatal(err)
	}
	if err := insertItem(ev, ctx(t), tenantA, "evd_1", "pay_1", "KNOWN", hashA, 300); err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]string{
		"update an item":    `UPDATE evidence_item SET fact_status = 'UNKNOWN' WHERE evidence_id = 'evd_1'`,
		"delete an item":    `DELETE FROM evidence_item WHERE evidence_id = 'evd_1'`,
		"truncate items":    `TRUNCATE evidence_item CASCADE`,
		"update a payload":  `UPDATE evidence_payload SET json = '{}' WHERE payload_ref = 'pay_1'`,
		"delete a payload":  `DELETE FROM evidence_payload WHERE payload_ref = 'pay_1'`,
		"truncate payloads": `TRUNCATE evidence_payload CASCADE`,
	} {
		// The administrator is the strongest caller there is: only the trigger can stop it.
		if _, err := w.admin.Exec(ctx(t), q); err == nil || !strings.HasPrefix(code(err), violation) {
			t.Errorf("%s by the administrator: err = %v (code %q), want an integrity error", name, err, code(err))
		}
	}
}

// INV-19, REQ-CTX-002: a status outside the eight, a "not credited" status and a hash that is not sha256 are refused by the table.
func TestTheTableRefusesAnInvalidItem(t *testing.T) {
	w := newWorld(t)
	ev := w.evidence(t, tenantA)
	if err := insertPayload(ev, ctx(t), tenantA, "pay_1"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		status, hash string
		freshness    int
	}{
		"a negative fact status":  {"NOT_CREDITED", hashA, 300},
		"a lower-case status":     {"known", hashA, 300},
		"a hash in upper case":    {"KNOWN", strings.ToUpper(hashA), 300},
		"a hash without the tag":  {"KNOWN", strings.TrimPrefix(hashA, "sha256:"), 300},
		"a zero freshness window": {"KNOWN", hashA, 0},
	}
	i := 0
	for name, c := range cases {
		i++
		err := insertItem(ev, ctx(t), tenantA, "evd_bad_"+string(rune('a'+i)), "pay_1", c.status, c.hash, c.freshness)
		if err == nil || !strings.HasPrefix(code(err), violation) {
			t.Errorf("%s: err = %v (code %q), want a constraint violation", name, err, code(err))
		}
	}
}

// INV-11, REQ-SEC-004: an item points only at a payload of its own tenant, and only at one that exists.
func TestAnItemPointsOnlyAtItsOwnTenantsPayload(t *testing.T) {
	w := newWorld(t)
	ev := w.evidence(t, tenantA)
	if err := insertPayload(w.evidence(t, "ten_bb"), ctx(t), "ten_bb", "pay_other"); err != nil {
		t.Fatal(err)
	}
	if err := insertItem(ev, ctx(t), tenantA, "evd_x", "pay_other", "KNOWN", hashA, 300); err == nil || !strings.HasPrefix(code(err), violation) {
		t.Errorf("an item of tenant A pointing at tenant B's payload: err = %v", err)
	}
	if err := insertItem(ev, ctx(t), tenantA, "evd_y", "pay_missing", "KNOWN", hashA, 300); err == nil || !strings.HasPrefix(code(err), violation) {
		t.Errorf("an item without a payload: err = %v", err)
	}
}

// BD-05, REQ-DATA-001: only the Evidence Layer writes evidence; the other powers may read it and may not write it.
func TestOnlyEvidenceRwWritesEvidence(t *testing.T) {
	w := newWorld(t)
	if err := insertPayload(w.evidence(t, tenantA), ctx(t), tenantA, "pay_1"); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "orchestrator_rw"} {
		p := w.as(t, role)
		if err := insertPayload(p, ctx(t), tenantA, "pay_"+role); code(err) != denied {
			t.Errorf("%s inserted a payload: err = %v", role, err)
		}
		if err := insertItem(p, ctx(t), tenantA, "evd_"+role, "pay_1", "KNOWN", hashA, 300); code(err) != denied {
			t.Errorf("%s inserted an item: err = %v", role, err)
		}
	}
	for _, role := range []string{"trust_rw", "outcome_rw"} {
		var n int
		if err := w.as(t, role).QueryRow(ctx(t), `SELECT count(*) FROM evidence_item`).Scan(&n); err != nil {
			t.Errorf("%s cannot read evidence_item: %v", role, err)
		}
	}
}

// INV-11, REQ-SEC-004, DEBT-04 option A: the database binds an Evidence write to the tenant of the login that makes it. A tenant A
// login cannot write either table for tenant B, and a login that is not mapped to a tenant (the administrator) cannot write at all.
func TestAnEvidenceLoginWritesOnlyItsOwnTenant(t *testing.T) {
	w := newWorld(t)
	a := w.evidence(t, tenantA)
	if err := insertPayload(w.evidence(t, "ten_bb"), ctx(t), "ten_bb", "pay_b"); err != nil {
		t.Fatal(err)
	}
	if err := insertPayload(a, ctx(t), "ten_bb", "pay_cross"); code(err) != denied {
		t.Errorf("a tenant A login wrote a payload for tenant B: err = %v", err)
	}
	if err := insertItem(a, ctx(t), "ten_bb", "evd_cross", "pay_b", "KNOWN", hashA, 300); code(err) != denied {
		t.Errorf("a tenant A login wrote an item for tenant B: err = %v", err)
	}
	if err := insertPayload(w.admin, ctx(t), tenantA, "pay_admin"); code(err) != denied {
		t.Errorf("an unmapped login wrote a payload: err = %v", err)
	}
	if n := w.count(t, `SELECT count(*) FROM evidence_payload WHERE payload_ref IN ('pay_cross', 'pay_admin')`); n != 0 {
		t.Errorf("%d refused rows were stored", n)
	}
}

func (w *world) count(t *testing.T, q string) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(t), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// INV-11, BD-05: even if a grant were widened by mistake, a login of another power cannot write evidence, because the guard checks
// the login's power as well as its tenant.
func TestAnotherPowersLoginIsRefusedEvenWithAMistakenGrant(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `GRANT INSERT ON evidence_payload TO trust_rw`); err != nil {
		t.Fatal(err)
	}
	trust := testpg.TenantLogin(t, w.admin, w.dsn, "trust_rw", tenantA)
	if err := insertPayload(trust, ctx(t), tenantA, "pay_trust"); code(err) != denied {
		t.Errorf("a trust login wrote evidence: err = %v", err)
	}
}

// INV-11, REQ-SEC-004 (Codex review of noema#167): reads are tenant-scoped by the database too. A tenant A login of Trust, Outcome or
// the Evidence Layer sees tenant A's evidence and none of tenant B's; a login with no tenant mapping sees nothing.
func TestEvidenceReadsAreScopedToTheLoginsTenant(t *testing.T) {
	w := newWorld(t)
	for _, tenant := range []string{tenantA, "ten_bb"} {
		if err := insertPayload(w.evidence(t, tenant), ctx(t), tenant, "pay_"+tenant); err != nil {
			t.Fatal(err)
		}
		if err := insertItem(w.evidence(t, tenant), ctx(t), tenant, "evd_"+tenant, "pay_"+tenant, "KNOWN", hashA, 300); err != nil {
			t.Fatal(err)
		}
	}
	readers := map[string]*pgxpool.Pool{
		"evidence_rw": w.evidence(t, tenantA),
		"trust_rw":    testpg.TenantLogin(t, w.admin, w.dsn, "trust_rw", tenantA),
		"outcome_rw":  testpg.TenantLogin(t, w.admin, w.dsn, "outcome_rw", tenantA),
	}
	for name, p := range readers {
		for _, table := range []string{"evidence_item", "evidence_payload"} {
			var own, other int
			if err := p.QueryRow(ctx(t), `SELECT count(*) FROM `+table+` WHERE tenant_id = 'ten_aa'`).Scan(&own); err != nil {
				t.Fatalf("%s reading %s: %v", name, table, err)
			}
			if err := p.QueryRow(ctx(t), `SELECT count(*) FROM `+table+` WHERE tenant_id = 'ten_bb'`).Scan(&other); err != nil {
				t.Fatalf("%s reading %s: %v", name, table, err)
			}
			if own != 1 || other != 0 {
				t.Errorf("%s reading %s: own tenant rows = %d (want 1), another tenant's rows = %d (want 0)", name, table, own, other)
			}
		}
	}
	var n int
	if err := w.as(t, "trust_rw").QueryRow(ctx(t), `SELECT count(*) FROM evidence_item`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a trust_rw session with no tenant mapping read %d items (%v), want 0", n, err)
	}
}

// INV-11, REQ-SEC-004 (security review of noema#167): the definer functions do not trust the session's schemas. A login that makes a
// temporary table named tenant_login, mapping itself to another tenant, gains neither a write nor a read for that tenant, because the
// functions name public.tenant_login and keep pg_temp out of their search path.
func TestATemporaryShadowOfTheTenantMappingChangesNothing(t *testing.T) {
	w := newWorld(t)
	if err := insertPayload(w.evidence(t, "ten_bb"), ctx(t), "ten_bb", "pay_b"); err != nil {
		t.Fatal(err)
	}
	a := w.evidence(t, tenantA)
	conn, err := a.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	var login string
	if err := conn.QueryRow(ctx(t), `SELECT session_user::text`).Scan(&login); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx(t), `CREATE TEMP TABLE tenant_login (login_role text, tenant_id text, power text)`); err != nil {
		t.Skipf("this login cannot create temporary tables, so the shadow cannot be set up: %v", err)
	}
	if _, err := conn.Exec(ctx(t), `INSERT INTO pg_temp.tenant_login VALUES ($1, 'ten_bb', 'evidence')`, login); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx(t), `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at)
		VALUES ('pay_shadow', 'ten_bb', '{}', $1, 'S2_internal', '2026-11-05T09:00:00Z')`, hashA); code(err) != denied {
		t.Errorf("a shadow mapping let a tenant A login write for tenant B: err = %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx(t), `SELECT count(*) FROM evidence_payload WHERE tenant_id = 'ten_bb'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a shadow mapping let a tenant A login read %d rows of tenant B (%v)", n, err)
	}
}

// REQ-SEC-004 (Security Reviewer's review of noema#167): row-level security is enabled and FORCED on both tables, so the table owner
// (the migration role) is held to the policy too.
func TestRowSecurityIsEnabledAndForcedOnBothEvidenceTables(t *testing.T) {
	w := newWorld(t)
	for _, table := range []string{"evidence_payload", "evidence_item"} {
		var on, forced bool
		if err := w.admin.QueryRow(ctx(t), `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`, table).Scan(&on, &forced); err != nil {
			t.Fatal(err)
		}
		if !on || !forced {
			t.Errorf("%s: row security enabled = %v, forced = %v, want both true", table, on, forced)
		}
	}
}
