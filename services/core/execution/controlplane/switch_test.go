package controlplane_test

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controlplane"
)

var admin = controlplane.Caller{Role: controlplane.RoleAdmin, PrincipalID: person}
var verifier = controlplane.Caller{Role: controlplane.RoleVerify, PrincipalID: "svc_verify"} // SYNTHETIC

func kinds(ev []stored) []string {
	var out []string
	for _, e := range ev {
		k := e.EventType
		if e.Kind != "" {
			k += ":" + e.Kind
		}
		out = append(out, k)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// REQ-EXE-008 (Audit): a set writes KILL_SWITCH_CHANGED as the admin emitter, naming the person, and turns the switch on for that tenant only.
func TestAdminSetsTheSwitchAndTheRecordNamesThePerson(t *testing.T) { // REQ-EXE-008, REQ-OPS-002, INV-03
	w := newWorld(t)
	if err := w.sw.Set(ctx(t), tenantA, corr, admin); err != nil {
		t.Fatalf("set: %v", err)
	}
	if !w.on(tenantA) || w.on(tenantB) {
		t.Fatalf("switch A = %v, B = %v; want A on and B off", w.on(tenantA), w.on(tenantB))
	}
	ev := w.events(tenantA)
	if got := kinds(ev); !equal(got, []string{"KILL_SWITCH_CHANGED"}) {
		t.Fatalf("events = %v; want exactly KILL_SWITCH_CHANGED", got)
	}
	if ev[0].WrittenBy != "admin" || ev[0].Actor != "admin:"+person {
		t.Fatalf("emitter %q, actor %q; want admin and admin:%s", ev[0].WrittenBy, ev[0].Actor, person)
	}
	if ev[0].Payload["kill_switch"] != true || ev[0].Payload["previous"] != false {
		t.Fatalf("payload %v; want kill_switch true and previous false", ev[0].Payload)
	}
	if n := len(w.events(tenantB)); n != 0 {
		t.Fatalf("tenant B has %d events; a set touches one tenant's chain", n)
	}
}

// The verification login (C-VERIFY) may set the switch too (Founder decision D5), and the event says who: no person, so the actor is the emitter's own identity.
func TestVerifierMaySetTheSwitch(t *testing.T) { // REQ-EXE-008, REQ-AUD-005
	w := newWorld(t)
	if err := w.sw.Set(ctx(t), tenantA, corr, verifier); err != nil {
		t.Fatalf("set: %v", err)
	}
	ev := w.events(tenantA)
	if !w.on(tenantA) || len(ev) != 1 || ev[0].Actor != "admin" {
		t.Fatalf("on = %v, events = %v; want on, one event, actor admin", w.on(tenantA), kinds(ev))
	}
}

// A clear by a person writes both events the PERD asks for, in the one transaction as the change.
func TestAdminClearsTheSwitchWithTwoEvents(t *testing.T) { // REQ-EXE-008, ruling A71
	w := newWorld(t)
	w.setAs(tenantA, true)
	if err := w.sw.Clear(ctx(t), tenantA, corr, admin); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if w.on(tenantA) {
		t.Fatal("the switch is still on after a clear by the administrator")
	}
	ev := w.events(tenantA)
	if got := kinds(ev); !equal(got, []string{"KILL_SWITCH_CHANGED", "SECURITY_EVENT:KILL_SWITCH_CLEARED"}) {
		t.Fatalf("events = %v; want KILL_SWITCH_CHANGED then SECURITY_EVENT KILL_SWITCH_CLEARED", got)
	}
	for _, e := range ev {
		if e.Actor != "admin:"+person || e.WrittenBy != "admin" {
			t.Fatalf("event %s: emitter %q actor %q; want admin and admin:%s", e.EventType, e.WrittenBy, e.Actor, person)
		}
	}
	if ev[0].Payload["kill_switch"] != false || ev[0].Payload["previous"] != true {
		t.Fatalf("payload %v; want kill_switch false and previous true", ev[0].Payload)
	}
}

// C-VERIFY clears nothing: the answer is ErrForbidden, the switch stays on, and one KILL_SWITCH_CLEAR_REFUSED is the only thing written.
func TestVerifierCannotClearAndTheRefusalIsRecorded(t *testing.T) { // REQ-EXE-008, REQ-AUD-005, INV-11
	w := newWorld(t)
	w.setAs(tenantA, true)
	err := w.sw.Clear(ctx(t), tenantA, corr, verifier)
	if !errors.Is(err, controlplane.ErrForbidden) {
		t.Fatalf("clear by the verifier: %v; want ErrForbidden", err)
	}
	if !w.on(tenantA) {
		t.Fatal("a refused clear turned the switch off")
	}
	if got := kinds(w.events(tenantA)); !equal(got, []string{"SECURITY_EVENT:KILL_SWITCH_CLEAR_REFUSED"}) {
		t.Fatalf("events = %v; want exactly SECURITY_EVENT KILL_SWITCH_CLEAR_REFUSED", got)
	}
}

// A caller with no role or no principal is refused before anything is read or written.
func TestAnUnidentifiedCallerChangesNothing(t *testing.T) { // REQ-EXE-008, INV-12
	w := newWorld(t)
	w.setAs(tenantA, true)
	for name, c := range map[string]controlplane.Caller{
		"no role":         {PrincipalID: person},
		"no principal":    {Role: controlplane.RoleAdmin},
		"an unknown role": {Role: controlplane.Role(99), PrincipalID: person},
	} {
		if err := w.sw.Clear(ctx(t), tenantA, corr, c); !errors.Is(err, controlplane.ErrCaller) {
			t.Errorf("%s, clear: %v; want ErrCaller", name, err)
		}
		if err := w.sw.Set(ctx(t), tenantB, corr, c); !errors.Is(err, controlplane.ErrCaller) {
			t.Errorf("%s, set: %v; want ErrCaller", name, err)
		}
	}
	if !w.on(tenantA) || w.on(tenantB) || len(w.events(tenantA))+len(w.events(tenantB)) != 0 {
		t.Fatal("an unidentified caller changed a switch or wrote an event")
	}
}

// EC-E16: when the Record cannot take the event, the switch does not change. Both directions.
func TestAFailedAppendLeavesTheSwitchAsItWas(t *testing.T) { // REQ-AUD-001, EC-E16, REQ-EXE-008
	w := newWorld(t)
	w.fail.set(true)
	if err := w.sw.Set(ctx(t), tenantA, corr, admin); err == nil {
		t.Fatal("set succeeded although the event could not be appended")
	}
	if w.on(tenantA) {
		t.Fatal("the switch is on although its event was not recorded")
	}
	w.setAs(tenantA, true)
	if err := w.sw.Clear(ctx(t), tenantA, corr, admin); err == nil {
		t.Fatal("clear succeeded although the event could not be appended")
	}
	if !w.on(tenantA) {
		t.Fatal("the switch is off although its clear was not recorded")
	}
	if n := len(w.events(tenantA)); n != 0 {
		t.Fatalf("%d events stored after failed appends", n)
	}
}

// INV-12: the tenant is the login's. A tenant with no admin login of its own is an error and no other tenant's login is used.
func TestATenantWithNoAdminLoginIsNotReachedThroughAnother(t *testing.T) { // INV-12, REQ-SEC-004
	w := newWorld(t)
	delete(w.admins, tenantB)
	if err := w.sw.Set(ctx(t), tenantB, corr, admin); err == nil {
		t.Fatal("set for a tenant with no login succeeded")
	}
	if w.on(tenantA) || w.on(tenantB) || len(w.events(tenantA))+len(w.events(tenantB)) != 0 {
		t.Fatal("a switch moved or an event was written")
	}
}

// REQ-EXE-008 (Expected behaviour): the switch is read from Postgres on every spend and never cached. The next transaction of the Trust and the Execution login
// of that tenant sees a set and a clear, and the other tenant's logins see nothing.
func TestTheNextTransactionOfTrustAndExecutionSeesTheChange(t *testing.T) { // REQ-EXE-008, PUS-06, INV-03
	w := newWorld(t)
	read := func(tenant string) (trustOn, execOn bool) {
		if err := w.trust[tenant].QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, tenant).Scan(&trustOn); err != nil {
			t.Fatal(err)
		}
		if err := w.exec[tenant].QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, tenant).Scan(&execOn); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := w.sw.Set(ctx(t), tenantA, corr, admin); err != nil {
		t.Fatal(err)
	}
	if a, e := read(tenantA); !a || !e {
		t.Fatalf("after a set: trust %v, execution %v; want both on", a, e)
	}
	if a, e := read(tenantB); a || e {
		t.Fatalf("tenant B after A's set: trust %v, execution %v; want both off", a, e)
	}
	if err := w.sw.Clear(ctx(t), tenantA, corr, admin); err != nil {
		t.Fatal(err)
	}
	if a, e := read(tenantA); a || e {
		t.Fatalf("after a clear: trust %v, execution %v; want both off", a, e)
	}
}

// A missing part is an error and not a default.
func TestAnIncompleteSwitchIsRefused(t *testing.T) { // AGENT-16
	w := newWorld(t)
	for name, s := range map[string]*controlplane.Switch{
		"no logins":   {Appender: w.sw.Appender, Timeout: w.sw.Timeout},
		"no appender": {Logins: w.admins, Timeout: w.sw.Timeout},
		"no timeout":  {Logins: w.admins, Appender: w.sw.Appender},
	} {
		if err := s.Set(ctx(t), tenantA, corr, admin); !errors.Is(err, controlplane.ErrConfig) {
			t.Errorf("%s: %v; want ErrConfig", name, err)
		}
	}
}
