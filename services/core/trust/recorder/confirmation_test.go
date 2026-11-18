package recorder_test

// TRU-07 follow-up (noema#603; Security Reviewer on noema#572, blocking 1; REQ-TRU-009, INV-25, REQ-SEC-004): an accepted confirmation that resolved to a fresh evaluation is recorded in
// confirmation_resolution with the binding hash it was given for and its expiry, so that the writer can check, in the database, that a confirmation exists for the tenant, has not
// expired and was given for exactly the action. The row is insert-only and tenant-scoped like the spend. Real PostgreSQL, real tenant logins. All data is SYNTHETIC.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

func TestRecordConfirmationRefusesWhatIsNotInTheContractsForm(t *testing.T) {
	w := newWorld(t)
	good := recorder.Confirmation{TenantID: tenant, EventID: "cfe_01FORM", RequestID: "cfr_01FORM", BindingHash: bindingHashOf(t, validBinding()),
		AcceptedAt: realNow().Add(-time.Minute), ExpiresAt: realNow().Add(time.Hour)}
	for name, mutate := range map[string]func(*recorder.Confirmation){
		"a tenant that is not a tenant ID": func(c *recorder.Confirmation) { c.TenantID = "aa" },
		"an event ID of another kind":      func(c *recorder.Confirmation) { c.EventID = "evt_01FORM" },
		"an empty event ID":                func(c *recorder.Confirmation) { c.EventID = "" },
		"a request ID of another kind":     func(c *recorder.Confirmation) { c.RequestID = "req_01FORM" },
		"a binding that is not a hash":     func(c *recorder.Confirmation) { c.BindingHash = "sha256:abc" },
		"no binding":                       func(c *recorder.Confirmation) { c.BindingHash = "" },
		"an expiry before the acceptance":  func(c *recorder.Confirmation) { c.ExpiresAt = c.AcceptedAt.Add(-time.Second) },
		"no expiry":                        func(c *recorder.Confirmation) { c.ExpiresAt = time.Time{} },
		"no acceptance time":               func(c *recorder.Confirmation) { c.AcceptedAt = time.Time{} },
	} {
		c := good
		mutate(&c)
		tx, err := w.trust.Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		err = recorder.RecordConfirmation(ctx(t), tx, c)
		_ = tx.Rollback(ctx(t))
		if !errors.Is(err, recorder.ErrRequest) {
			t.Errorf("%s: err %v, want ErrRequest", name, err)
		}
	}
	if n := w.count(t, `SELECT count(*) FROM confirmation_resolution`); n != 0 {
		t.Errorf("%d confirmations were recorded from refused requests", n)
	}
}

// A confirmation event is recorded once for a tenant, and the row is insert-only: the administrator is refused by the trigger itself, the Trust login by its privileges.
func TestAConfirmationIsRecordedOnceAndIsInsertOnly(t *testing.T) {
	w := newWorld(t)
	w.confirmed(t, given)
	if err := recordConfirmation(t, w.trust, tenant, given, validBinding(), realNow().Add(time.Hour)); err == nil {
		t.Error("the same confirmation event was recorded twice")
	}
	// a request is answered once: another event for the same request is refused
	b := validBinding()
	if err := recordConfirmation(t, w.trust, tenant, "cfe_01SECOND", b, realNow().Add(time.Hour)); err != nil {
		t.Fatalf("a confirmation of another request: %v", err)
	}
	dup := recorder.Confirmation{TenantID: tenant, EventID: "cfe_01THIRD", RequestID: "cfr_" + strings.TrimPrefix(given, "cfe_"), BindingHash: bindingHashOf(t, b), AcceptedAt: realNow().Add(-time.Minute), ExpiresAt: realNow().Add(time.Hour)}
	tx, err := w.trust.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	err = recorder.RecordConfirmation(ctx(t), tx, dup)
	_ = tx.Rollback(ctx(t))
	if err == nil {
		t.Error("a second answer to the same request was recorded")
	}
	for name, stmt := range map[string]string{
		"update":   `UPDATE confirmation_resolution SET expires_at = expires_at + interval '1 day'`,
		"delete":   `DELETE FROM confirmation_resolution`,
		"truncate": `TRUNCATE confirmation_resolution`,
	} {
		_, err := w.admin.Exec(ctx(t), stmt)
		if code, msg := pgRefusal(t, name+" by the administrator", err); code != sqlstateIntegrity || !strings.Contains(msg, "insert-only") {
			t.Errorf("%s by the administrator: %s %q, want %s from the insert-only trigger", name, code, msg, sqlstateIntegrity)
		}
		_, err = w.trust.Exec(ctx(t), stmt)
		if code, _ := pgRefusal(t, name+" by the Trust login", err); code != sqlstatePrivilege {
			t.Errorf("%s by the Trust login: %s, want %s (no privilege)", name, code, sqlstatePrivilege)
		}
	}
	if n := w.count(t, `SELECT count(*) FROM confirmation_resolution`); n != 2 {
		t.Errorf("%d confirmations remain after the refused changes, want 2", n)
	}
}

// The tenant binding of a recorded confirmation, each layer on its own, as for the spend: the tenant guard, then row-level security; and another tenant's login sees none.
func TestARecordedConfirmationIsScopedToItsTenantByTheGuardAndByRowSecurityEachOnItsOwn(t *testing.T) {
	w := newWorld(t)
	w.confirmed(t, given)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	forTenantBB := func() error {
		return recordConfirmation(t, w.trust, "ten_bb", "cfe_01FORBB", validBinding(), realNow().Add(time.Hour))
	}
	switchGuard := func(state string) {
		t.Helper()
		if _, err := w.admin.Exec(ctx(t), `ALTER TABLE confirmation_resolution `+state+` TRIGGER confirmation_resolution_tenant_guard`); err != nil {
			t.Fatal(err)
		}
	}
	code, msg := pgRefusal(t, "the tenant guard", forTenantBB())
	if code != sqlstatePrivilege || !strings.Contains(msg, "may not") || !strings.Contains(msg, "ten_bb") {
		t.Errorf("ten_aa's login recording for ten_bb: %s %q, want %s from the tenant guard", code, msg, sqlstatePrivilege)
	}
	switchGuard("DISABLE")
	code, msg = pgRefusal(t, "row-level security", forTenantBB())
	if code != sqlstatePrivilege || !strings.Contains(msg, "row-level security") {
		t.Errorf("with the guard off: %s %q, want %s from row-level security", code, msg, sqlstatePrivilege)
	}
	switchGuard("ENABLE")
	if n := w.count(t, `SELECT count(*) FROM confirmation_resolution WHERE tenant_id = 'ten_bb'`); n != 0 {
		t.Errorf("%d confirmations exist for ten_bb", n)
	}
	other := newTenantTrust(t, w, "ten_bb")
	var seen int
	if err := other.QueryRow(ctx(t), `SELECT count(*) FROM confirmation_resolution`).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if seen != 0 {
		t.Errorf("ten_bb's Trust login sees %d of ten_aa's confirmations", seen)
	}
}
