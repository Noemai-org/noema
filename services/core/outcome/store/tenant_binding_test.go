package store_test

// SEC-08 / SR's P1 on noema#234: outcome_record_verdict takes the tenant from the connecting login, not from its p_tenant argument. REQ-SEC-004, INV-12,
// DEBT-08. Outcome's login for one tenant must not be able to write a verdict, a Record event or an outbox row for another tenant, whatever it passes as
// p_tenant. Synthetic tenants and rows.

import (
	"testing"
)

func TestAnOutcomeLoginCannotRecordAVerdictForAnotherTenant(t *testing.T) {
	w := newWorld(t)
	w.seed(t, other, "grt_other", "rcp_other") // the other tenant has a spent grant and a receipt of its own
	mine := w.outcome(t)                       // the login mapped to `tenant`

	foreign := achieved("ver_foreign")
	foreign.tenant, foreign.receiptID = other, "rcp_other"
	err := foreign.call(ctx(t), mine)
	if sqlState(err) != "42501" {
		t.Fatalf("a verdict for tenant %q through the login of tenant %q: got %v, want 42501 (insufficient_privilege)", other, tenant, err)
	}
	for _, q := range []string{
		`SELECT count(*) FROM outcome_verdict WHERE tenant_id = $1`,
		`SELECT count(*) FROM record_event WHERE tenant_id = $1`,
		`SELECT count(*) FROM outbox_event WHERE tenant_id = $1`,
	} {
		if n := w.count(t, q, other); n != 0 {
			t.Errorf("%s: %d rows written for the other tenant by a refused call", q, n)
		}
	}
	// The same login still records a verdict for its own tenant.
	if err := achieved("ver_own").call(ctx(t), mine); err != nil {
		t.Fatalf("a verdict for the login's own tenant must still be recorded: %v", err)
	}
}
