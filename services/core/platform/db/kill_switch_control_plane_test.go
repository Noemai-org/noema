package db_test

// EXE-06, the control plane's database half (REQ-EXE-008, REQ-OPS-002; INV-03, INV-11, INV-12; ruling A71, interim, relayed, not yet committed): the tenant's admin emitter login
// (migration 0011, the emitter `admin`) sets and clears its own tenant's kill switch through kill_switch_apply_t, which takes the tenant from the login and returns the previous
// state. The guard of migration 0017 stays: no other login clears the switch, whatever it is granted. Synthetic tenants.

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

func TestTheAdminLoginSetsAndClearsItsOwnTenantOnlyAndLearnsThePreviousState(t *testing.T) { // REQ-EXE-008, ruling A71
	admin, dsn := migratedDSN(t)
	for _, id := range []string{"ten_a", "ten_b"} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	ctl := testpg.EmitterLogin(t, admin, dsn, "ten_a", "admin")
	var prev bool
	if err := ctl.QueryRow(ctx(t), `SELECT kill_switch_apply_t(true)`).Scan(&prev); err != nil || prev {
		t.Fatalf("set: previous %v, %v; want false and no error", prev, err)
	}
	if err := ctl.QueryRow(ctx(t), `SELECT kill_switch_apply_t(true)`).Scan(&prev); err != nil || !prev {
		t.Fatalf("set again: previous %v, %v; want true and no error", prev, err)
	}
	for tenant, want := range map[string]bool{"ten_a": true, "ten_b": false} {
		var on bool
		if err := admin.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, tenant).Scan(&on); err != nil || on != want {
			t.Fatalf("%s kill_switch = %v, %v; want %v", tenant, on, err, want)
		}
	}
	if err := ctl.QueryRow(ctx(t), `SELECT kill_switch_apply_t(false)`).Scan(&prev); err != nil || !prev {
		t.Fatalf("clear: previous %v, %v; want true and no error", prev, err)
	}
	var on bool
	if err := admin.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = 'ten_a'`).Scan(&on); err != nil || on {
		t.Fatalf("ten_a kill_switch = %v, %v after a clear; want off", on, err)
	}
}

// The function is the only way: the same login cannot UPDATE or read the table, so a clear cannot be made by a statement of its own.
func TestTheAdminLoginHasNoDirectPrivilegeOnTheTenantTable(t *testing.T) { // REQ-EXE-008, INV-11
	admin, dsn := migratedDSN(t)
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil {
		t.Fatal(err)
	}
	ctl := testpg.EmitterLogin(t, admin, dsn, "ten_a", "admin")
	_, err := ctl.Exec(ctx(t), `UPDATE tenant SET kill_switch = false WHERE tenant_id = 'ten_a'`)
	wantCode(t, err, denied, "the admin login updating tenant directly")
	var n int
	err = ctl.QueryRow(ctx(t), `SELECT count(*) FROM tenant`).Scan(&n)
	wantCode(t, err, denied, "the admin login reading tenant directly")
	var on bool
	if err := admin.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = 'ten_a'`).Scan(&on); err != nil || !on {
		t.Fatalf("kill_switch = %v, %v; a refused statement must change nothing", on, err)
	}
}

// Every other login is refused by the function itself, with the switch on or off: a login of another emitter, of a power, of the verification group.
func TestNoOtherLoginCanApplyTheSwitchThroughTheFunction(t *testing.T) { // REQ-EXE-008, INV-11, INV-12
	admin, dsn := migratedDSN(t)
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil {
		t.Fatal(err)
	}
	edge := testpg.EmitterLogin(t, admin, dsn, "ten_a", "core-edge")
	trust := testpg.TenantLogin(t, admin, dsn, "trust_rw", "ten_a")
	exec := testpg.TenantLogin(t, admin, dsn, "execution_rw", "ten_a")
	ver := testpg.VerifierLogin(t, admin, dsn)
	for _, c := range []struct {
		name string
		run  func(on bool) error
	}{
		{"core-edge", func(on bool) error {
			var b bool
			return edge.QueryRow(ctx(t), `SELECT kill_switch_apply_t($1)`, on).Scan(&b)
		}},
		{"trust_rw", func(on bool) error {
			var b bool
			return trust.QueryRow(ctx(t), `SELECT kill_switch_apply_t($1)`, on).Scan(&b)
		}},
		{"execution_rw", func(on bool) error {
			var b bool
			return exec.QueryRow(ctx(t), `SELECT kill_switch_apply_t($1)`, on).Scan(&b)
		}},
		{"record_verify", func(on bool) error {
			var b bool
			return ver.QueryRow(ctx(t), `SELECT kill_switch_apply_t($1)`, on).Scan(&b)
		}},
	} {
		for _, on := range []bool{true, false} {
			wantCode(t, c.run(on), denied, c.name+" applying the kill switch")
		}
	}
	var on bool
	if err := admin.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = 'ten_a'`).Scan(&on); err != nil || !on {
		t.Fatalf("kill_switch = %v, %v; refused calls must change nothing", on, err)
	}
}

// Only the admin emitter's logins may execute the function: the privilege is on the group record_emitter and the function checks the emitter, so a PUBLIC or a power
// role holds no EXECUTE.
func TestOnlyTheEmitterGroupCanExecuteTheApplyFunction(t *testing.T) { // REQ-EXE-008, INV-11
	admin, _ := migratedDSN(t)
	for role, want := range map[string]bool{
		"public": false, "trust_rw": false, "execution_rw": false, "outcome_rw": false, "evidence_rw": false, "orchestrator_rw": false,
		"record_read": false, "record_verify": false, "record_emitter": true,
	} {
		var ok bool
		q := `SELECT has_function_privilege($1, 'kill_switch_apply_t(boolean)', 'EXECUTE')`
		if role == "public" {
			q = `SELECT has_function_privilege('public', 'kill_switch_apply_t(boolean)', 'EXECUTE')`
		}
		var err error
		if role == "public" {
			err = admin.QueryRow(ctx(t), q).Scan(&ok)
		} else {
			err = admin.QueryRow(ctx(t), q, role).Scan(&ok)
		}
		if err != nil || ok != want {
			t.Errorf("%s EXECUTE on kill_switch_apply_t = %v, %v; want %v", role, ok, err, want)
		}
	}
}
