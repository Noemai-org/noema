package db_test

// EDG-01, DEBT-07 (docs#106 R16; Architect's decision on Q-DEBT04-1, option b): a login per emitter that is not a power. The mapping row of
// such a login is made only by provision_emitter_login, callable only by the migrating role; the login holds the Record append functions
// and writes only the emitter it is bound to, only in its own tenant.

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// groupRolesForProvisioning are the group roles a running service can act as.
var groupRolesForProvisioning = []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "record_append", "record_read", "record_verify", "record_emitter"}

func createEmitterRole(t *testing.T, w *tenantWorld, name string, groups ...string) {
	t.Helper()
	q := `CREATE ROLE ` + pgx.Identifier{name}.Sanitize() + ` LOGIN`
	if len(groups) > 0 {
		q += ` IN ROLE ` + pgx.Identifier{groups[0]}.Sanitize()
		for _, g := range groups[1:] {
			q += `, ` + pgx.Identifier{g}.Sanitize()
		}
	}
	if _, err := w.admin.Exec(ctx(t), q); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = w.admin.Exec(ctx(t), `DROP ROLE IF EXISTS `+pgx.Identifier{name}.Sanitize()) })
}

// The provisioned login writes as its emitter in its tenant and as nothing else.
func TestProvisionEmitterLoginBindsALoginToOneEmitterInOneTenant(t *testing.T) {
	w := newTenantWorld(t)
	login := "emit_edge_" + w.suffix
	createEmitterRole(t, w, login, "record_emitter")
	if _, err := w.admin.Exec(ctx(t), `SELECT provision_emitter_login($1, 'ten_a', 'core-edge')`, login); err != nil {
		t.Fatalf("a valid emitter login was refused: %v", err)
	}
	var power, emitter, tenant string
	if err := w.admin.QueryRow(ctx(t), `SELECT power, emitter, tenant_id FROM tenant_login WHERE login_role = $1`, login).Scan(&power, &emitter, &tenant); err != nil ||
		power != "record_emitter" || emitter != "core-edge" || tenant != "ten_a" {
		t.Fatalf("mapping = %q, %q, %q, %v", power, emitter, tenant, err)
	}
	pool := w.login(t, login)
	insert := func(emitter, bodyTenant string) error {
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
		_, err = tx.Exec(ctx(t), `SELECT record_insert_t('recev_z', $1, $2, $3, $4, 'SESSION_VALIDATED', 'ses_1', 'sha256:p', 'corr_1', now(), '3.0', $4, jsonb_build_object('tenant_id', $5::text), NULL)`,
			seq+1, head, hashOf("7"), emitter, bodyTenant)
		return err
	}
	if err := insert("core-edge", "ten_a"); err != nil {
		t.Fatalf("the bound login could not write as its emitter: %v", err)
	}
	for _, other := range []string{"core-execution", "core-record", "admin", "model-service", "mock-ledger", ""} {
		wantCode(t, insert(other, "ten_a"), denied, "the core-edge login writing as "+other)
	}
	wantCode(t, insert("core-edge", "ten_b"), denied, "the core-edge login of ten_a writing an event for ten_b")
}

// The function checks the login, its groups, the emitter and the tenant.
func TestProvisionEmitterLoginRefusesWhatIsNotAnEmitterLogin(t *testing.T) {
	w := newTenantWorld(t)
	ok := "emit_ok_" + w.suffix
	nologin := "emit_nologin_" + w.suffix
	power := "emit_power_" + w.suffix
	both := "emit_both_" + w.suffix
	createEmitterRole(t, w, ok, "record_emitter")
	createEmitterRole(t, w, power, "execution_rw")
	createEmitterRole(t, w, both, "record_emitter", "execution_rw")
	// Codex review of noema#181: nothing but record_emitter, whatever else the role could reach.
	readAll := "emit_readall_" + w.suffix
	monitor := "emit_monitor_" + w.suffix
	other := "emit_other_" + w.suffix
	createEmitterRole(t, w, readAll, "record_emitter", "pg_read_all_data")
	createEmitterRole(t, w, monitor, "record_emitter", "pg_monitor")
	if _, err := w.admin.Exec(ctx(t), `CREATE ROLE `+pgx.Identifier{other + "_grp"}.Sanitize()+` NOLOGIN`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = w.admin.Exec(ctx(t), `DROP ROLE IF EXISTS `+pgx.Identifier{other + "_grp"}.Sanitize()) })
	createEmitterRole(t, w, other, "record_emitter", other+"_grp")
	attrs := map[string]string{"superuser": "SUPERUSER", "createrole": "CREATEROLE", "createdb": "CREATEDB", "replication": "REPLICATION", "bypassrls": "BYPASSRLS"}
	attrRoles := map[string]string{}
	for name, attr := range attrs {
		role := "emit_" + name + "_" + w.suffix
		if _, err := w.admin.Exec(ctx(t), `CREATE ROLE `+pgx.Identifier{role}.Sanitize()+` LOGIN `+attr+` IN ROLE record_emitter`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = w.admin.Exec(ctx(t), `DROP ROLE IF EXISTS `+pgx.Identifier{role}.Sanitize()) })
		attrRoles[name] = role
	}
	if _, err := w.admin.Exec(ctx(t), `CREATE ROLE `+pgx.Identifier{nologin}.Sanitize()+` NOLOGIN IN ROLE record_emitter`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = w.admin.Exec(ctx(t), `DROP ROLE IF EXISTS `+pgx.Identifier{nologin}.Sanitize()) })
	cases := map[string][]any{
		"a role that does not exist":            {"no_such_" + w.suffix, "ten_a", "core-edge"},
		"a role that cannot log in":             {nologin, "ten_a", "core-edge"},
		"a login of a power group only":         {power, "ten_a", "core-edge"},
		"an emitter login that is also a power": {both, "ten_a", "core-edge"},
		"a member of pg_read_all_data":          {readAll, "ten_a", "core-edge"},
		"a member of pg_monitor":                {monitor, "ten_a", "core-edge"},
		"a member of another role":              {other, "ten_a", "core-edge"},
		"a superuser":                           {attrRoles["superuser"], "ten_a", "core-edge"},
		"a login with CREATEROLE":               {attrRoles["createrole"], "ten_a", "core-edge"},
		"a login with CREATEDB":                 {attrRoles["createdb"], "ten_a", "core-edge"},
		"a login with REPLICATION":              {attrRoles["replication"], "ten_a", "core-edge"},
		"a login with BYPASSRLS":                {attrRoles["bypassrls"], "ten_a", "core-edge"},
		"a power's own identity (execution)":    {ok, "ten_a", "core-execution"},
		"a power's own identity (trust)":        {ok, "ten_a", "core-trust"},
		"a power's own identity (outcome)":      {ok, "ten_a", "core-outcome"},
		"a power's own identity (evidence)":     {ok, "ten_a", "core-evidence"},
		"a power's own identity (workflows)":    {ok, "ten_a", "core-workflows"},
		"an empty emitter":                      {ok, "ten_a", ""},
		"an upper-case emitter":                 {ok, "ten_a", "TRUST"},
		"an emitter with a space":               {ok, "ten_a", "core edge"},
		"a tenant that does not exist":          {ok, "ten_zz", "core-edge"},
	}
	for name, args := range cases {
		if _, err := w.admin.Exec(ctx(t), `SELECT provision_emitter_login($1, $2, $3)`, args...); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := w.admin.Exec(ctx(t), `SELECT provision_emitter_login($1, 'ten_a', 'core-edge')`, ok); err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `SELECT provision_emitter_login($1, 'ten_b', 'core-record')`, ok); err == nil {
		t.Error("the same login mapped twice was accepted")
	}
}

// Only the migrating role can call it: no runtime role, no tenant login, no emitter login and not PUBLIC.
func TestNoRuntimeRoleCanExecuteProvisionEmitterLogin(t *testing.T) {
	w := newTenantWorld(t)
	for _, role := range append(append([]string{}, groupRolesForProvisioning...), "public") {
		var ok bool
		if err := w.admin.QueryRow(ctx(t), `SELECT has_function_privilege($1, 'provision_emitter_login(text, text, text)', 'EXECUTE')`, role).Scan(&ok); err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if ok && role != "record_append" { // record_append owns the function and has no login
			t.Errorf("%s can execute provision_emitter_login", role)
		}
	}
	xa := w.login(t, w.name("execution_rw", "ten_a"))
	_, err := xa.Exec(ctx(t), `SELECT provision_emitter_login('x', 'ten_a', 'core-edge')`)
	wantCode(t, err, denied, "a tenant login calling provision_emitter_login")
}

// Codex review of noema#181: the function accepts exactly the runtime emitters of record/chain/actors.json that are not a power, and
// nothing else. The expected set is read from that file, so the migration's list cannot drift from the contract.
func TestProvisionEmitterLoginAcceptsExactlyTheRuntimeEmittersThatAreNotPowers(t *testing.T) {
	raw, err := os.ReadFile("../../record/chain/actors.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Emitters []struct{ Name, Scope string } `json:"emitters"`
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	powers := map[string]bool{"core-trust": true, "core-execution": true, "core-outcome": true, "core-evidence": true, "core-workflows": true}
	w := newTenantWorld(t)
	accepted, refused := 0, 0
	for i, e := range table.Emitters {
		login := fmt.Sprintf("emit_set_%d_%s", i, w.suffix)
		createEmitterRole(t, w, login, "record_emitter")
		_, err := w.admin.Exec(ctx(t), `SELECT provision_emitter_login($1, 'ten_a', $2)`, login, e.Name)
		want := e.Scope == "RUNTIME" && !powers[e.Name]
		switch {
		case want && err != nil:
			t.Errorf("%s (runtime, not a power) was refused: %v", e.Name, err)
		case !want && err == nil:
			t.Errorf("%s (scope %s, power %v) was accepted", e.Name, e.Scope, powers[e.Name])
		}
		if want {
			accepted++
		} else {
			refused++
		}
	}
	if accepted == 0 || refused == 0 {
		t.Fatalf("the table gave %d accepted and %d refused; the test did not exercise both", accepted, refused)
	}
}
