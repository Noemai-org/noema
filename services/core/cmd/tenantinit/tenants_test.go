package main

// TRU-02 (ruling A484): the tenant rows the command creates. REQ-SEC-004 (tenant isolation; the tenant list is created by the operator's superuser, migration 0030), INV-11 (the kill
// switch is never cleared by a service), INV-12, AGENT-13 and A347 (fixed public error texts). All data is SYNTHETIC.

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

func TestTenantsAreCreatedOnceAndTwice(t *testing.T) { // REQ-SEC-004
	w := newWorld(t)
	a := w.args("1", tenantA+"=Synthetic Bank A", tenantB+"=Synthetic Bank B")
	first := w.run(a...)
	if first.code != exitDone || first.out != outLine(2, 0, 1, 0) || first.err != "" {
		t.Fatalf("first run: code %d, stdout %q, stderr %q; want 0, %q and nothing", first.code, first.out, first.err, outLine(2, 0, 1, 0))
	}
	got := w.tenants()
	want := map[string]tenantRow{tenantA: {name: "Synthetic Bank A", timezone: "UTC"}, tenantB: {name: "Synthetic Bank B", timezone: "UTC"}}
	if len(got) != 2 || got[tenantA] != want[tenantA] || got[tenantB] != want[tenantB] {
		t.Fatalf("tenant rows %v, want %v (the kill switch off and the time zone the column default)", got, want)
	}
	second := w.run(a...)
	if second.code != exitDone || second.out != outLine(0, 2, 0, 1) || second.err != "" {
		t.Fatalf("second run: code %d, stdout %q, stderr %q; want 0, %q and nothing", second.code, second.out, second.err, outLine(0, 2, 0, 1))
	}
	if n := w.count(`SELECT count(*) FROM tenant`); n != 2 {
		t.Fatalf("%d tenant rows after two runs, want exactly 2", n)
	}
	if n := w.count(`SELECT count(*) FROM signing_key`); n != 1 {
		t.Fatalf("%d signing keys after two runs, want exactly 1", n)
	}
}

func TestANewTenantJoinsAnExistingOne(t *testing.T) { // REQ-SEC-004: a second run with one more tenant creates only that one
	w := newWorld(t)
	if r := w.run(w.args("1", tenantA+"=Synthetic Bank A")...); r.code != exitDone || r.out != outLine(1, 0, 1, 0) {
		t.Fatalf("first run: %+v", r)
	}
	r := w.run(w.args("1", tenantA+"=Synthetic Bank A", tenantB+"=Synthetic Bank B")...)
	if r.code != exitDone || r.out != outLine(1, 1, 0, 1) {
		t.Fatalf("second run: code %d, stdout %q; want 0 and %q", r.code, r.out, outLine(1, 1, 0, 1))
	}
	if n := len(w.tenants()); n != 2 {
		t.Fatalf("%d tenant rows, want 2", n)
	}
}

func TestANameMayContainAnEqualsSignAndNonASCIICharacters(t *testing.T) { // the ID ends at the first "="; the name is the rest
	w := newWorld(t)
	name := "Société a=b (Lagos)"
	if r := w.run(w.args("1", tenantA+"="+name)...); r.code != exitDone {
		t.Fatalf("code %d, stderr %q", r.code, r.err)
	}
	if got := w.tenants()[tenantA].name; got != name {
		t.Fatalf("stored name %q, want %q", got, name)
	}
}

func TestSixteenTenantsAndASixtyFourCharacterNameAreAccepted(t *testing.T) { // the bounds of the interface
	w := newWorld(t)
	var tenants []string
	for i := 0; i < 16; i++ {
		tenants = append(tenants, "ten_b"+string(rune('a'+i))+"="+strings.Repeat("é", 64))
	}
	r := w.run(w.args("1", tenants...)...)
	if r.code != exitDone || r.out != outLine(16, 0, 1, 0) {
		t.Fatalf("code %d, stdout %q, stderr %q; want 0 and %q", r.code, r.out, r.err, outLine(16, 0, 1, 0))
	}
}

func TestAnExistingTenantUnderAnotherNameIsRefusedAndNothingIsChanged(t *testing.T) { // REQ-SEC-004, AGENT-13, A347
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Original Name')`, tenantA); err != nil {
		t.Fatal(err)
	}
	// The second tenant and the key are valid and come before and after the refused one: none of them may be kept (one transaction).
	r := w.run(w.args("1", tenantB+"=Synthetic Bank B", tenantA+"=Other Name")...)
	if r.code != exitRefused || r.out != "" || r.err != textNameDiffers+"\n" {
		t.Fatalf("code %d, stdout %q, stderr %q; want 1, nothing and %q", r.code, r.out, r.err, textNameDiffers+"\n")
	}
	mustContainNone(t, "output", r.all(), "Original Name", "Other Name", "Synthetic Bank B")
	got := w.tenants()
	if len(got) != 1 || got[tenantA].name != "Original Name" {
		t.Fatalf("tenant rows %v, want only %s under its original name", got, tenantA)
	}
	if n := len(w.keys()); n != 0 {
		t.Fatalf("%d signing keys kept after a refusal, want none", n)
	}
}

func TestTheKillSwitchAndTheTimeZoneOfAnExistingTenantAreNeverChanged(t *testing.T) { // INV-11
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name, kill_switch, timezone) VALUES ($1, 'Same Name', true, 'Africa/Lagos')`, tenantA); err != nil {
		t.Fatal(err)
	}
	r := w.run(w.args("1", tenantA+"=Same Name")...)
	if r.code != exitDone || r.out != outLine(0, 1, 1, 0) {
		t.Fatalf("code %d, stdout %q, stderr %q; want 0 and %q", r.code, r.out, r.err, outLine(0, 1, 1, 0))
	}
	if got := w.tenants()[tenantA]; got != (tenantRow{name: "Same Name", timezone: "Africa/Lagos", killSwitch: true}) {
		t.Fatalf("tenant row %+v: the kill switch or the time zone of an existing tenant was changed", got)
	}
}

func TestARuntimeLoginCannotCreateTenantsOrRegisterKeys(t *testing.T) { // REQ-SEC-004, REQ-SEC-005, A347: the database refuses, the command says so in fixed words
	for _, group := range []string{"trust_rw", "execution_rw"} {
		t.Run(group, func(t *testing.T) {
			w := newWorld(t)
			if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Existing')`, tenantA); err != nil {
				t.Fatal(err)
			}
			login := testpg.TenantLogin(t, w.admin, w.dsn, group, tenantA)
			dsnPath := w.dsnFile("runtime", withPassword(t, login.Config().ConnString()))
			a := w.args("1", tenantA+"=Existing", tenantB+"=Synthetic Bank B")
			a[1] = dsnPath
			r := w.run(a...)
			if r.code != exitRefused || r.out != "" || r.err != textLoginDenied+"\n" {
				t.Fatalf("code %d, stdout %q, stderr %q; want 1, nothing and %q", r.code, r.out, r.err, textLoginDenied+"\n")
			}
			mustContainNone(t, "output", strings.ToLower(r.all()), "permission denied", "row-level", "violates", "sqlstate", markerPassword, group)
			if got := w.tenants(); len(got) != 1 {
				t.Fatalf("tenant rows %v, want only the one that existed", got)
			}
			if n := len(w.keys()); n != 0 {
				t.Fatalf("%d signing keys registered by a runtime login, want none", n)
			}
		})
	}
}
