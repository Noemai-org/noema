package db_test

// DEBT-04 option A, slice 1 (REQ-SEC-004, INV-12, REQ-AUD-001, REQ-AUD-003): the tenant of a Record call comes from the
// login the connection authenticated as, never from an argument. These tests connect as real LOGIN roles, each mapped to
// one tenant, because SET ROLE would leave session_user as the administrator and prove nothing. All data is SYNTHETIC.

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

type tenantWorld struct {
	admin  *pgxpool.Pool
	dsn    string
	suffix string // login roles are cluster-wide, so each test database gets its own names
}

// name is the login role for a power and a tenant in this world.
func (w *tenantWorld) name(power, tenant string) string {
	return power + "__" + tenant + "_" + w.suffix
}

func newTenantWorld(t *testing.T) *tenantWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	w := &tenantWorld{admin: admin, dsn: dsn, suffix: strings.TrimPrefix(u.Path, "/")}
	roles := map[string]string{ // login role -> group role
		w.name("execution_rw", "ten_a"): "execution_rw", w.name("execution_rw", "ten_b"): "execution_rw", w.name("execution_rw", "unmapped"): "execution_rw",
		w.name("record_read", "ten_a"): "record_read", w.name("record_read", "ten_b"): "record_read",
	}
	for role, group := range roles {
		if _, err := admin.Exec(ctx(t), `CREATE ROLE `+pgx.Identifier{role}.Sanitize()+` LOGIN IN ROLE `+group); err != nil {
			t.Fatalf("create %s: %v", role, err)
		}
		role := role
		t.Cleanup(func() {
			c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if a, err := pgxpool.New(c, strings.Replace(dsn, "/"+w.suffix+"?", "/postgres?", 1)); err == nil {
				_, _ = a.Exec(c, `DROP ROLE IF EXISTS `+pgx.Identifier{role}.Sanitize())
				a.Close()
			}
		})
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A'), ('ten_b', 'B')`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant_login (login_role, power, tenant_id) VALUES ($1, 'execution', 'ten_a'), ($2, 'execution', 'ten_b'), ($3, 'record_read', 'ten_a'), ($4, 'record_read', 'ten_b')`,
		w.name("execution_rw", "ten_a"), w.name("execution_rw", "ten_b"), w.name("record_read", "ten_a"), w.name("record_read", "ten_b")); err != nil {
		t.Fatal(err)
	}
	return w
}

// login opens a pool whose connections authenticate as the given LOGIN role.
func (w *tenantWorld) login(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	u, err := url.Parse(w.dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User(role)
	pool, err := pgxpool.New(ctx(t), u.String())
	if err != nil {
		t.Fatalf("login as %s: %v", role, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

const insertT = `SELECT record_insert_t($1, $2, $3, $4, 'core-execution', 'GRANT_SPENT', 'grt_1', 'sha256:p', 'corr_1', $5, '3.0', 'core-execution', jsonb_build_object('tenant_id', $6::text), NULL)`

// appendAs appends one event through the login's own tenant-bound functions and returns the new head hash.
func (w *tenantWorld) appendAs(t *testing.T, pool *pgxpool.Pool, tenant, id string, seqWant int64, hash string) {
	t.Helper()
	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	var seq int64
	var head string
	if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &head); err != nil {
		t.Fatalf("lock head: %v", err)
	}
	if seq+1 != seqWant {
		t.Fatalf("head seq %d, want %d", seq, seqWant-1)
	}
	if _, err := tx.Exec(ctx(t), insertT, id, seqWant, head, hash, time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC), tenant); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
}

func hashOf(n string) string { return "sha256:" + strings.Repeat(n, 64) }

// A login writes to its own tenant's chain, whatever else is in the database, and two tenants' chains stay separate.
func TestTenantBoundWritesGoToTheLoginsOwnTenant(t *testing.T) {
	w := newTenantWorld(t)
	a, b := w.login(t, w.name("execution_rw", "ten_a")), w.login(t, w.name("execution_rw", "ten_b"))
	w.appendAs(t, a, "ten_a", "recev_a1", 1, hashOf("1"))
	w.appendAs(t, a, "ten_a", "recev_a2", 2, hashOf("2"))
	w.appendAs(t, b, "ten_b", "recev_b1", 1, hashOf("3"))
	rows, err := w.admin.Query(ctx(t), `SELECT event_id, tenant_id FROM record_event ORDER BY event_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var id, ten string
		if err := rows.Scan(&id, &ten); err != nil {
			t.Fatal(err)
		}
		got[id] = ten
	}
	want := map[string]string{"recev_a1": "ten_a", "recev_a2": "ten_a", "recev_b1": "ten_b"}
	if len(got) != len(want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	for id, ten := range want {
		if got[id] != ten {
			t.Fatalf("event %s is in tenant %q, want %q", id, got[id], ten)
		}
	}
}

// The tenant-bound functions have no tenant argument, so there is nothing for a caller to choose.
func TestTenantBoundFunctionsTakeNoTenantArgument(t *testing.T) {
	w := newTenantWorld(t)
	rows, err := w.admin.Query(ctx(t), `SELECT p.proname, coalesce(array_to_string(p.proargnames, ','), '') FROM pg_proc p
		WHERE p.proname IN ('record_lock_head_t', 'record_insert_t', 'record_read_chain_t', 'record_login_tenant') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var name, args string
		if err := rows.Scan(&name, &args); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(args, "tenant") && name != "record_read_chain_t" {
			t.Errorf("%s has an argument that names a tenant: %s", name, args)
		}
		n++
	}
	if n != 4 {
		t.Fatalf("found %d of the 4 functions", n)
	}
}

// A read login sees its own tenant's chain and only that. This is the cross-tenant denial of DEBT-04 for the new path.
func TestTenantBoundReadSeesOnlyTheLoginsTenant(t *testing.T) {
	w := newTenantWorld(t)
	a, b := w.login(t, w.name("execution_rw", "ten_a")), w.login(t, w.name("execution_rw", "ten_b"))
	w.appendAs(t, a, "ten_a", "recev_a1", 1, hashOf("1"))
	w.appendAs(t, a, "ten_a", "recev_a2", 2, hashOf("2"))
	w.appendAs(t, b, "ten_b", "recev_b1", 1, hashOf("3"))
	for role, want := range map[string][]string{w.name("record_read", "ten_a"): {"recev_a1", "recev_a2"}, w.name("record_read", "ten_b"): {"recev_b1"}} {
		rows, err := w.login(t, role).Query(ctx(t), `SELECT event_id FROM record_read_chain_t()`)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		var got []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			got = append(got, id)
		}
		rows.Close()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s read %v, want %v", role, got, want)
		}
	}
}

// Refusals under the real logins: no mapping, the wrong kind of call, direct table access, the mapping table, and the ways a
// caller might try to become another login or tenant.
func TestTenantBoundCallsAreRefusedForTheWrongLogin(t *testing.T) {
	w := newTenantWorld(t)
	xa, xb, un := w.name("execution_rw", "ten_a"), w.name("execution_rw", "ten_b"), w.name("execution_rw", "unmapped")
	ra := w.name("record_read", "ten_a")
	q := func(r string) string { return pgx.Identifier{r}.Sanitize() }
	cases := []struct{ name, role, stmt string }{
		{"an unmapped login cannot lock a head", un, `SELECT * FROM record_lock_head_t()`},
		{"a read login cannot write", ra, `SELECT * FROM record_lock_head_t()`},
		{"a write login cannot read the chain", xa, `SELECT * FROM record_read_chain_t()`},
		{"the mapping table cannot be read", xa, `SELECT * FROM tenant_login`},
		{"the mapping cannot be changed", xa, `UPDATE tenant_login SET tenant_id = 'ten_b' WHERE login_role = '` + xa + `'`},
		{"a mapping cannot be added", xa, `INSERT INTO tenant_login VALUES ('` + un + `', 'execution', 'ten_a')`},
		{"the resolver cannot be called directly", xa, `SELECT record_login_tenant('write')`},
		{"the Record tables cannot be read", xa, `SELECT * FROM record_event`},
		{"session authorization cannot be switched", xa, `SET SESSION AUTHORIZATION ` + q(xb)},
		{"another tenant's login cannot be assumed", xa, `SET ROLE ` + q(xb)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := w.login(t, c.role).Exec(ctx(t), c.stmt)
			wantCode(t, err, denied, c.name)
		})
	}
}

// Switching to the group role changes current_user, not session_user: the tenant stays the login's.
func TestSetRoleToTheGroupRoleDoesNotChangeTheTenant(t *testing.T) {
	w := newTenantWorld(t)
	a := w.login(t, w.name("execution_rw", "ten_a"))
	conn, err := a.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx(t), `SET ROLE execution_rw`); err != nil {
		t.Fatal(err)
	}
	var seq int64
	var head string
	if err := conn.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &head); err != nil {
		t.Fatal(err)
	}
	var tenant string
	if err := w.admin.QueryRow(ctx(t), `SELECT tenant_id FROM record_chain_head WHERE hash = $1`, head).Scan(&tenant); err != nil || tenant != "ten_a" {
		t.Fatalf("after SET ROLE the head belongs to %q (%v), want ten_a", tenant, err)
	}
}

// An event whose hashed body names another tenant than the login's is refused by the database, so the chain of the login's
// tenant can never hold an event that says it belongs to another (it would otherwise break verification).
func TestAnEventNamingAnotherTenantIsRefused(t *testing.T) {
	w := newTenantWorld(t)
	a := w.login(t, w.name("execution_rw", "ten_a"))
	tx, err := a.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	var seq int64
	var head string
	if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &head); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx(t), insertT, "recev_x1", seq+1, head, hashOf("9"), time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC), "ten_b")
	wantCode(t, err, denied, "an event naming ten_b written by ten_a's login")
}

// Security review of noema#145, finding 1 (REQ-SEC-004, INV-12, REQ-AUD-003): record_insert_t refuses a written_by that is not
// the emitter of the login's power. Without this, a login of one power could write Record events in another power's name.
// Powers map to emitters as PERD 41.1 names them: trust to core-trust, execution to core-execution, outcome to core-outcome,
// evidence to core-evidence, orchestrator to core-workflows. Emitters that are not a power are open question Q-DEBT04-1.
func TestRecordInsertRefusesAnEmitterThatIsNotTheLoginsPower(t *testing.T) {
	w := newTenantWorld(t)
	a := w.login(t, w.name("execution_rw", "ten_a"))
	for _, emitter := range []string{"core-trust", "core-outcome", "core-evidence", "core-workflows"} {
		t.Run("execution login as "+emitter, func(t *testing.T) {
			tx, err := a.Begin(ctx(t))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx(t)) }()
			var seq int64
			var head string
			if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &head); err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx(t), `SELECT record_insert_t('recev_x', $1, $2, $3, $4, 'GRANT_SPENT', 'grt_1', 'sha256:p', 'corr_1', now(), '3.0', $4, jsonb_build_object('tenant_id', 'ten_a'), NULL)`,
				seq+1, head, hashOf("9"), emitter)
			wantCode(t, err, denied, "execution login writing as "+emitter)
		})
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM record_event`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a refused insert left %d rows (%v)", n, err)
	}
}
