package main

// TRU-02 (ruling A489, amending A484): the command creates no tenant row and touches none. Tenant rows have one creator, create-tenants.sh (A353; the guard is
// deploy/compose/postgres/test_provision_env.sh). REQ-SEC-004 (tenant isolation), INV-11 (the kill switch is never cleared by a service), INV-12, AGENT-13 and A347. All data is SYNTHETIC.

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

func TestARunCreatesNoTenantRow(t *testing.T) { // A489, REQ-SEC-004
	w := newWorld(t)
	for i := 1; i <= 2; i++ {
		if r := w.run(w.args("1")...); r.code != exitDone || r.err != "" {
			t.Fatalf("run %d: code %d, stderr %q", i, r.code, r.err)
		}
		if got := w.tenants(); len(got) != 0 {
			t.Fatalf("run %d: tenant rows %v, want none: tenant rows have one creator, create-tenants.sh", i, got)
		}
	}
	if n := w.count(`SELECT count(*) FROM signing_key`); n != 1 {
		t.Fatalf("%d signing keys after two runs, want exactly 1", n)
	}
}

func TestExistingTenantRowsAreLeftAsTheyAre(t *testing.T) { // A489, INV-11: the kill switch and the time zone of a tenant created by create-tenants.sh are not touched
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name, kill_switch, timezone) VALUES ($1, 'Created Elsewhere', true, 'Africa/Lagos')`, tenantA); err != nil {
		t.Fatal(err)
	}
	r := w.run(w.args("1")...)
	if r.code != exitDone || r.out != outLine(1, 0) {
		t.Fatalf("code %d, stdout %q, stderr %q; want 0 and %q", r.code, r.out, r.err, outLine(1, 0))
	}
	got := w.tenants()
	if len(got) != 1 || got[tenantA] != (tenantRow{name: "Created Elsewhere", timezone: "Africa/Lagos", killSwitch: true}) {
		t.Fatalf("tenant rows %v: a row was added, changed or removed", got)
	}
}

func TestARuntimeLoginCannotRegisterKeys(t *testing.T) { // REQ-SEC-004, REQ-SEC-005, A347: the database refuses, the command says so in fixed words
	for _, group := range []string{"trust_rw", "execution_rw"} {
		t.Run(group, func(t *testing.T) {
			w := newWorld(t)
			if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Existing')`, tenantA); err != nil {
				t.Fatal(err)
			}
			login := testpg.TenantLogin(t, w.admin, w.dsn, group, tenantA)
			dsnPath := w.dsnFile("runtime", withPassword(t, login.Config().ConnString()))
			a := w.args("1")
			a[1] = dsnPath
			r := w.run(a...)
			if r.code != exitRefused || r.out != "" || r.err != textLoginDenied+"\n" {
				t.Fatalf("code %d, stdout %q, stderr %q; want 1, nothing and %q", r.code, r.out, r.err, textLoginDenied+"\n")
			}
			mustContainNone(t, "output", r.all(), "permission denied", "row-level", "violates", "SQLSTATE", markerPassword, group)
			if n := len(w.keys()); n != 0 {
				t.Fatalf("%d signing keys registered by a runtime login, want none", n)
			}
		})
	}
}
