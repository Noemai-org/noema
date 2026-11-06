package db_test

// DEBT-04 option A, slice 3 (REQ-AUD-005, REQ-SEC-004, INV-12): the verifier's privileges, the provisioning of tenant logins,
// and the rule that no migration creates a role (init.sh does; the migrating role cannot). All data is SYNTHETIC.

import (
	"io/fs"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/migrations"
)

var createRole = regexp.MustCompile(`(?i)\bCREATE\s+ROLE\b`)

// The migrating role (noema_migration_login in Compose) cannot create roles, so a migration that needs CREATE ROLE cannot run there.
// 0001 predates the rule: its creation is guarded and is a no-op because init.sh creates the roles first. Every later
// migration must only grant.
func TestNoMigrationAfter0001CreatesARole(t *testing.T) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil || len(names) < 3 {
		t.Fatalf("migrations: %v %v", names, err)
	}
	checked := 0
	for _, n := range names {
		if strings.HasPrefix(n, "0001_") {
			continue
		}
		b, err := fs.ReadFile(migrations.FS, n)
		if err != nil {
			t.Fatal(err)
		}
		// Comments may mention the words; the statement is what counts.
		var code []string
		for _, line := range strings.Split(string(b), "\n") {
			if i := strings.Index(line, "--"); i >= 0 {
				line = line[:i]
			}
			code = append(code, line)
		}
		if createRole.MatchString(strings.Join(code, "\n")) {
			t.Errorf("%s contains CREATE ROLE: create the role in deploy/compose/postgres/init.sh", n)
		}
		checked++
	}
	if checked < 3 {
		t.Fatalf("checked only %d migrations", checked)
	}
}

func loginAs(t *testing.T, dsn, role string) *pgxpool.Pool {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User(role)
	pool, err := pgxpool.New(ctx(t), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// The verifier reads every tenant's chain, heads and the tenant list, can switch a tenant's writes off, and can do nothing else.
func TestRecordVerifyReadsEverythingAndChangesOnlyTheKillSwitch(t *testing.T) {
	w := newTenantWorld(t)
	a, b := w.login(t, w.name("execution_rw", "ten_a")), w.login(t, w.name("execution_rw", "ten_b"))
	w.appendAs(t, a, "ten_a", "recev_a1", 1, hashOf("1"))
	w.appendAs(t, b, "ten_b", "recev_b1", 1, hashOf("2"))

	verifier := "verify_" + w.suffix
	if _, err := w.admin.Exec(ctx(t), `CREATE ROLE `+pgx.Identifier{verifier}.Sanitize()+` LOGIN IN ROLE record_verify`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = w.admin.Exec(ctx(t), `DROP ROLE IF EXISTS `+pgx.Identifier{verifier}.Sanitize()) })
	v := loginAs(t, w.dsn, verifier)

	var events, heads, tenants int
	if err := v.QueryRow(ctx(t), `SELECT (SELECT count(*) FROM record_event), (SELECT count(*) FROM record_chain_head), (SELECT count(*) FROM tenant)`).Scan(&events, &heads, &tenants); err != nil {
		t.Fatalf("the verifier could not read: %v", err)
	}
	if events != 2 || heads != 2 || tenants != 2 {
		t.Fatalf("the verifier read %d events, %d heads, %d tenants; want 2, 2, 2", events, heads, tenants)
	}
	if _, err := v.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil {
		t.Fatalf("the verifier could not switch writes off: %v", err)
	}
	for _, stmt := range []string{
		`UPDATE tenant SET name = 'x'`,
		`INSERT INTO tenant (tenant_id, name) VALUES ('ten_c', 'C')`,
		`DELETE FROM tenant`,
		`UPDATE record_event SET hash = 'x'`,
		`DELETE FROM record_event`,
		`INSERT INTO record_event (event_id, tenant_id, seq, prev_hash, hash, written_by, event_type, object_ref, payload_hash, correlation_id, at, schema_version, actor, body)
		 VALUES ('recev_x', 'ten_a', 9, 'p', 'h', 'w', 'T', 'o', 'ph', 'c', now(), '3.0', 'a', '{}')`,
		`UPDATE record_chain_head SET seq = 99`,
		`SELECT * FROM record_lock_head_t()`,
		`SELECT * FROM decision_grant`,
		`SELECT * FROM tenant_login`,
		`SELECT provision_tenant_login('x', 'execution', 'ten_a')`,
	} {
		_, err := v.Exec(ctx(t), stmt)
		wantCode(t, err, denied, "the verifier: "+stmt)
	}
}

// provision_tenant_login maps an existing login to a tenant, checks the login and its power, and nobody but the migrating
// role can call it.
func TestProvisionTenantLoginChecksTheLoginAndItsPower(t *testing.T) {
	w := newTenantWorld(t)
	good := "prov_ok_" + w.suffix
	other := "prov_other_" + w.suffix
	nologin := "prov_nologin_" + w.suffix
	for _, q := range []string{
		`CREATE ROLE ` + pgx.Identifier{good}.Sanitize() + ` LOGIN IN ROLE execution_rw`,
		`CREATE ROLE ` + pgx.Identifier{other}.Sanitize() + ` LOGIN IN ROLE outcome_rw`,
		`CREATE ROLE ` + pgx.Identifier{nologin}.Sanitize() + ` NOLOGIN IN ROLE execution_rw`,
	} {
		if _, err := w.admin.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, r := range []string{good, other, nologin} {
			_, _ = w.admin.Exec(ctx(t), `DROP ROLE IF EXISTS `+pgx.Identifier{r}.Sanitize())
		}
	})
	if _, err := w.admin.Exec(ctx(t), `SELECT provision_tenant_login($1, 'execution', 'ten_a')`, good); err != nil {
		t.Fatalf("a valid login was refused: %v", err)
	}
	var tenant, power string
	if err := w.admin.QueryRow(ctx(t), `SELECT tenant_id, power FROM tenant_login WHERE login_role = $1`, good).Scan(&tenant, &power); err != nil || tenant != "ten_a" || power != "execution" {
		t.Fatalf("mapping = %q, %q, %v; want ten_a, execution", tenant, power, err)
	}
	for name, args := range map[string][]any{
		"a login of another power":       {other, "execution", "ten_a"},
		"a role that cannot log in":      {nologin, "execution", "ten_a"},
		"a role that does not exist":     {"no_such_" + w.suffix, "execution", "ten_a"},
		"a tenant that does not exist":   {good + "x", "execution", "ten_zz"},
		"a power that does not exist":    {good, "root", "ten_a"},
		"the same login mapped twice":    {good, "execution", "ten_b"},
		"a record_read login as a power": {good, "record_read", "ten_a"},
	} {
		if _, err := w.admin.Exec(ctx(t), `SELECT provision_tenant_login($1, $2, $3)`, args...); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	xa := w.login(t, w.name("execution_rw", "ten_a"))
	_, err := xa.Exec(ctx(t), `SELECT provision_tenant_login($1, 'execution', 'ten_b')`, good)
	wantCode(t, err, denied, "a tenant login calling provision_tenant_login")
}

// Security review of noema#145, finding 2: provision_tenant_login runs as the migrating role (the provisioning script uses
// noema_migration_login). No group role, so no runtime login, and not PUBLIC may execute it.
func TestNoRuntimeRoleCanExecuteProvisionTenantLogin(t *testing.T) {
	w := newTenantWorld(t)
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "record_read", "record_verify", "record_append", "public"} {
		var ok bool
		if err := w.admin.QueryRow(ctx(t), `SELECT has_function_privilege($1, 'provision_tenant_login(text, text, text)', 'EXECUTE')`, role).Scan(&ok); err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if ok && role != "record_append" { // record_append owns the function; it has no login
			t.Errorf("%s can execute provision_tenant_login", role)
		}
	}
}
