package db_test

// Migration 0030 (A273, A280; SEC-08, REQ-SEC-004; INV-11, INV-12; DEBT-43): row-level security with FORCE on the `tenant` table. Until then every power login held SELECT
// on `tenant` with no policy, so any tenant's Trust, Execution, Outcome, Evidence or Orchestrator login could list every tenant's ID and name (the Security Reviewer's P2 on
// noema#658). The tenant of a row is the tenant of the login that connected (decision_login_tenant(), session_user); the chain verifier (record_verify) is the one role that
// reads every tenant and may set, never clear, a tenant's kill switch. All data is SYNTHETIC.

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

// The roles that hold SELECT on `tenant` since migration 0001, each reached through a login that is mapped to one tenant.
var tenantReaderGroups = []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw"}

var syntheticTenants = []struct{ id, name string }{
	{"ten_a", "Synthetic Bank Alpha"},
	{"ten_b", "Synthetic Bank Beta"},
	{"ten_c", "Synthetic Bank Gamma"},
}

func seedSyntheticTenants(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()
	for _, s := range syntheticTenants {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $2)`, s.id, s.name); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx(t), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// (a) REQ-SEC-004: each power login sees exactly its own tenant's row and cannot list the others, by ID or by name, however it asks.
// Mutations: a policy that is USING (true), a policy for the wrong role group, no row security at all: each shows another tenant's row here.
func TestEachPowerLoginSeesExactlyItsOwnTenantRow(t *testing.T) {
	admin, dsn := migratedDSN(t)
	seedSyntheticTenants(t, admin)
	for _, group := range tenantReaderGroups {
		for _, own := range syntheticTenants {
			t.Run(group+"/"+own.id, func(t *testing.T) {
				login := testpg.TenantLogin(t, admin, dsn, group, own.id)
				rows, err := login.Query(ctx(t), `SELECT tenant_id, name FROM tenant ORDER BY 1`)
				if err != nil {
					t.Fatalf("%s of %s lists the tenant table: %v", group, own.id, err)
				}
				var seen []string
				for rows.Next() {
					var id, name string
					if err := rows.Scan(&id, &name); err != nil {
						t.Fatal(err)
					}
					seen = append(seen, id+"/"+name)
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				if want := own.id + "/" + own.name; len(seen) != 1 || seen[0] != want {
					t.Fatalf("%s of %s sees %v, want exactly [%s]", group, own.id, seen, want)
				}
				for _, other := range syntheticTenants {
					if other.id == own.id {
						continue
					}
					var s string
					if err := login.QueryRow(ctx(t), `SELECT name FROM tenant WHERE tenant_id = $1`, other.id).Scan(&s); !errors.Is(err, pgx.ErrNoRows) {
						t.Errorf("%s of %s reads the name of %s: %q, %v", group, own.id, other.id, s, err)
					}
					var k bool
					if err := login.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, other.id).Scan(&k); !errors.Is(err, pgx.ErrNoRows) {
						t.Errorf("%s of %s reads the kill switch of %s: %v, %v", group, own.id, other.id, k, err)
					}
					if n := count(t, login, `SELECT count(*) FROM tenant WHERE name = $1`, other.name); n != 0 {
						t.Errorf("%s of %s finds %d rows by the name of %s", group, own.id, n, other.id)
					}
				}
				if n := count(t, login, `SELECT count(*) FROM tenant WHERE name LIKE 'Synthetic%'`); n != 1 {
					t.Errorf("%s of %s counts %d tenants by a name prefix, want its own only", group, own.id, n)
				}
				// The privileges are unchanged by the policy: no power login writes the registry.
				for _, stmt := range []string{
					`INSERT INTO tenant (tenant_id, name) VALUES ('ten_zz', 'Z')`,
					`UPDATE tenant SET name = 'renamed'`,
					`DELETE FROM tenant`,
				} {
					_, err := login.Exec(ctx(t), stmt)
					wantCode(t, err, denied, group+": "+stmt)
				}
			})
		}
	}
}

// A session that no tenant_login row maps (the administrator's session with SET ROLE, or a login the deployment forgot to map) gets no tenant from decision_login_tenant(),
// so the policy compares tenant_id with NULL and shows nothing: it fails closed. SET ROLE drops the superuser's bypass, because row security follows the effective role.
func TestAPowerRoleWithNoTenantMappingSeesNoTenantRow(t *testing.T) {
	admin := migrated(t)
	seedSyntheticTenants(t, admin)
	for _, group := range tenantReaderGroups {
		tx, err := admin.Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx(t), `SET LOCAL ROLE `+pgx.Identifier{group}.Sanitize()); err != nil {
			_ = tx.Rollback(ctx(t))
			t.Fatal(err)
		}
		var n int
		err = tx.QueryRow(ctx(t), `SELECT count(*) FROM tenant`).Scan(&n)
		_ = tx.Rollback(ctx(t))
		if err != nil || n != 0 {
			t.Errorf("%s with no tenant mapping counts %d tenants (%v), want none", group, n, err)
		}
	}
}

// A login that holds no privilege on `tenant` still holds none: the policy opens no door by itself.
func TestTheRecordReadLoginStillHoldsNoPrivilegeOnTheTenantTable(t *testing.T) {
	admin, dsn := migratedDSN(t)
	seedSyntheticTenants(t, admin)
	login := testpg.TenantLogin(t, admin, dsn, "record_read", "ten_a")
	_, err := login.Exec(ctx(t), `SELECT tenant_id FROM tenant`)
	wantCode(t, err, denied, "record_read reading tenant")
}

// (b) The reads of the kill switch (E6 and E8 of Execution, T3 of Trust) go through the tenant's own login: they see their own tenant's switch, and a tenant that is not theirs
// is "no row", which both callers treat as writes disabled (fail closed). The verifier's set reaches the tenant's own readers.
func TestTheKillSwitchOfATenantIsReadByItsOwnLoginsOnly(t *testing.T) {
	admin, dsn := migratedDSN(t)
	seedSyntheticTenants(t, admin)
	ver := testpg.VerifierLogin(t, admin, dsn)
	if tag, err := ver.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("the verifier sets the switch of ten_a: %v, %d rows", err, tag.RowsAffected())
	}
	for _, group := range []string{"execution_rw", "trust_rw"} {
		a := testpg.TenantLogin(t, admin, dsn, group, "ten_a")
		b := testpg.TenantLogin(t, admin, dsn, group, "ten_b")
		var k bool
		if err := a.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = 'ten_a'`).Scan(&k); err != nil || !k {
			t.Errorf("%s of ten_a reads its own switch: %v, %v; want true", group, k, err)
		}
		if err := b.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = 'ten_b'`).Scan(&k); err != nil || k {
			t.Errorf("%s of ten_b reads its own switch: %v, %v; want false (ten_a's stop does not stop ten_b)", group, k, err)
		}
		if err := b.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = 'ten_a'`).Scan(&k); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("%s of ten_b reads the switch of ten_a: %v, %v", group, k, err)
		}
	}
}

// (c) The chain verifier (REC-05) lists every tenant, by the same query verify.go runs, sets a tenant's kill switch and writes nothing else.
func TestTheVerifierListsEveryTenantAndSetsTheKillSwitchUnderItsOwnPolicy(t *testing.T) {
	admin, dsn := migratedDSN(t)
	seedSyntheticTenants(t, admin)
	ver := testpg.VerifierLogin(t, admin, dsn)
	rows, err := ver.Query(ctx(t), `SELECT tenant_id FROM tenant UNION SELECT tenant_id FROM record_chain_head UNION SELECT tenant_id FROM record_event ORDER BY 1`)
	if err != nil {
		t.Fatalf("the verifier lists the tenants: %v", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if strings.Join(ids, ",") != "ten_a,ten_b,ten_c" {
		t.Fatalf("the verifier lists %v, want every tenant", ids)
	}
	if n := count(t, ver, `SELECT count(*) FROM tenant WHERE name LIKE 'Synthetic%'`); n != 3 {
		t.Fatalf("the verifier counts %d tenant rows, want 3", n)
	}
	for _, id := range []string{"ten_a", "ten_c"} {
		if tag, err := ver.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = $1`, id); err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("the verifier stops %s: %v, %d rows", id, err, tag.RowsAffected())
		}
	}
	// Setting it again changes one row, which is what verify.halt requires of a tenant that is already stopped.
	if tag, err := ver.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("the verifier stops a stopped tenant again: %v, %d rows", err, tag.RowsAffected())
	}
	if n := count(t, admin, `SELECT count(*) FROM tenant WHERE kill_switch`); n != 2 {
		t.Fatalf("%d tenants are stopped, want ten_a and ten_c", n)
	}
	for _, stmt := range []string{
		`INSERT INTO tenant (tenant_id, name) VALUES ('ten_zz', 'Z')`,
		`UPDATE tenant SET name = 'renamed'`,
		`DELETE FROM tenant`,
	} {
		_, err := ver.Exec(ctx(t), stmt)
		wantCode(t, err, denied, "the verifier: "+stmt)
	}
}

// The verifier's UPDATE policy is the second lock on the kill switch (the trigger of migration 0017 is the first): its check lets a row through only if the switch is on, so
// it produces no row with the switch off even if the trigger were missing. The trigger is disabled here, by the administrator, to show the policy alone.
func TestTheVerifiersPolicyAloneRefusesToClearTheKillSwitch(t *testing.T) {
	admin, dsn := migratedDSN(t)
	seedSyntheticTenants(t, admin)
	ver := testpg.VerifierLogin(t, admin, dsn)
	if _, err := admin.Exec(ctx(t), `ALTER TABLE tenant DISABLE TRIGGER tenant_kill_switch_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil {
		t.Fatal(err)
	}
	_, err := ver.Exec(ctx(t), `UPDATE tenant SET kill_switch = false WHERE tenant_id = 'ten_a'`)
	wantCode(t, err, denied, "the verifier clearing the switch with the trigger off")
	if err != nil && !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("the refusal is %v, want row-level security", err)
	}
	if n := count(t, admin, `SELECT count(*) FROM tenant WHERE kill_switch AND tenant_id = 'ten_a'`); n != 1 {
		t.Fatal("a refused clear changed the switch")
	}
}

type tenantPolicy struct{ name, cmd, roles, using, check string }

func policiesOnTenant(t *testing.T, pool *pgxpool.Pool) []tenantPolicy {
	t.Helper()
	rows, err := pool.Query(ctx(t), `
		SELECT p.polname, p.polcmd::text,
		       coalesce((SELECT string_agg(r.rolname, ',' ORDER BY r.rolname) FROM pg_roles r WHERE r.oid = ANY (p.polroles)), 'PUBLIC'),
		       coalesce(pg_get_expr(p.polqual, p.polrelid), ''), coalesce(pg_get_expr(p.polwithcheck, p.polrelid), '')
		  FROM pg_policy p WHERE p.polrelid = 'public.tenant'::regclass ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []tenantPolicy
	for rows.Next() {
		var p tenantPolicy
		if err := rows.Scan(&p.name, &p.cmd, &p.roles, &p.using, &p.check); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// (d) The catalog: `tenant` has row security enabled and forced, and exactly five policies, one per role group (the five power groups, the verifier's read, the verifier's stop) and the control plane's two, which match the admin emitter login's own tenant (migration 0042, EXE-06). The expectation is the whole set, so a policy that is loosened, widened to another role, added or dropped fails it.
func TestTenantForcesRowSecurityWithOnePolicyPerRoleGroup(t *testing.T) {
	pool := migrated(t)
	var enabled, forced bool
	if err := pool.QueryRow(ctx(t), `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = 'public.tenant'::regclass`).Scan(&enabled, &forced); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced {
		t.Errorf("tenant: row security enabled=%v forced=%v, want both", enabled, forced)
	}
	want := []tenantPolicy{
		{"tenant_control_plane_apply", "w", "record_append", "(tenant_id = admin_login_tenant())", "(tenant_id = admin_login_tenant())"},
		{"tenant_control_plane_read", "r", "record_append", "(tenant_id = admin_login_tenant())", ""},
		{"tenant_own_row", "*", "evidence_rw,execution_rw,orchestrator_rw,outcome_rw,trust_rw", "(tenant_id = decision_login_tenant())", "(tenant_id = decision_login_tenant())"},
		{"tenant_verify_halt", "w", "record_verify", "true", "kill_switch"},
		{"tenant_verify_read", "r", "record_verify", "true", ""},
	}
	got := policiesOnTenant(t, pool)
	sort.Slice(want, func(i, j int) bool { return want[i].name < want[j].name })
	if len(got) != len(want) {
		t.Fatalf("tenant has %d policies %+v, want %d %+v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("policy %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Foreign keys to `tenant` are checked as the table's owner. With FORCE the owner is subject to the policies, so this pins that PostgreSQL still lets the check read the row it
// needs (the check runs with row security off for the owner, SECURITY_NOFORCE_RLS) when the owner is NOT a superuser, as in the deployment where the migration role owns the
// schema. The owner is moved to noema_migrator, a role that is not a superuser; a mapping row (tenant_login references tenant) is then written, and one for a tenant that does
// not exist is still refused (the check really runs). The same owner, without a tenant mapping, reads no tenant row: FORCE applies to it, which is the administration path of
// migration 0018 (a person who administers the tenant list is a superuser, as for the kill switch).
func TestForcedRowSecurityOnTenantDoesNotBreakForeignKeyChecksOfANonSuperuserOwner(t *testing.T) {
	admin, dsn := migratedDSN(t)
	seedSyntheticTenants(t, admin)
	if _, err := admin.Exec(ctx(t), `ALTER TABLE tenant OWNER TO noema_migrator`); err != nil {
		t.Fatal(err)
	}
	var super bool
	if err := admin.QueryRow(ctx(t), `SELECT rolsuper FROM pg_roles WHERE rolname = 'noema_migrator'`).Scan(&super); err != nil || super {
		t.Fatalf("noema_migrator superuser = %v, %v; the test needs a role that is not one", super, err)
	}
	// TenantLogin maps a new login to ten_a: an INSERT into tenant_login, whose foreign key to tenant is checked as the owner.
	testpg.TenantLogin(t, admin, dsn, "execution_rw", "ten_a")
	_, err := admin.Exec(ctx(t), `INSERT INTO tenant_login (login_role, power, tenant_id) VALUES ('execution_rw__nobody', 'execution', 'ten_nobody')`)
	wantCode(t, err, "23503", "a mapping row for a tenant that does not exist")
	tx, err := admin.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := tx.Exec(ctx(t), `SET LOCAL ROLE noema_migrator`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(ctx(t), `SELECT count(*) FROM tenant`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the owner with no tenant mapping counts %d tenants (%v), want none: FORCE applies to the owner", n, err)
	}
}

// Tenant creation is an init-time step of the operator's superuser (ruling on noema#716): no policy and no role exists for it. The migration login is not a superuser, so under FORCE
// it cannot insert a tenant row, and a login of a power cannot either. This documents the intended behaviour: it passes the day 0030 lands and fails if a policy is added for the owner.
func TestOnlyASuperuserCreatesATenant(t *testing.T) {
	admin, _ := migratedDSN(t)
	if _, err := admin.Exec(ctx(t), `ALTER TABLE tenant OWNER TO noema_migrator`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_su', 'Created by the superuser')`); err != nil {
		t.Fatalf("the superuser creates a tenant: %v", err)
	}
	tx, err := admin.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := tx.Exec(ctx(t), `SET LOCAL ROLE noema_migrator`); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_mig', 'Created by the migration role')`)
	wantCode(t, err, denied, "the non-superuser owner inserting a tenant under FORCE")
}
