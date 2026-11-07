package db_test

// REC-05 / D5 (Founder decision, 7 Nov 2026): the chain verifier may set a tenant's kill switch; only a person clears it. REQ-AUD-005, INV-11, INV-12.
// The verification login holds UPDATE on tenant.kill_switch so that it can stop a tenant after a break (migration 0006); that privilege must not let it
// clear the switch, whether the break was real or the verifier was fooled. Synthetic tenants.

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

func TestTheVerifierSetsTheKillSwitchButCannotClearIt(t *testing.T) {
	admin, dsn := migratedDSN(t)
	for _, id := range []string{"ten_a", "ten_b"} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	ver := testpg.VerifierLogin(t, admin, dsn)

	// Setting is allowed, for any tenant: the verifier is all-tenant by design (named exception, SR F1).
	if _, err := ver.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil {
		t.Fatalf("the verifier must be able to set the kill switch: %v", err)
	}
	// Setting it again is not a clear.
	if _, err := ver.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`); err != nil {
		t.Fatalf("setting an already set switch is not a clear: %v", err)
	}
	// Clearing is refused, for the tenant it stopped and in a statement that also sets another.
	for _, stmt := range []string{
		`UPDATE tenant SET kill_switch = false WHERE tenant_id = 'ten_a'`,
		`UPDATE tenant SET kill_switch = (tenant_id = 'ten_b')`,
	} {
		_, err := ver.Exec(ctx(t), stmt)
		wantCode(t, err, denied, "the verifier clearing the kill switch: "+stmt)
	}
	var on bool
	if err := admin.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = 'ten_a'`).Scan(&on); err != nil || !on {
		t.Fatalf("ten_a kill_switch = %v, %v; a refused clear must change nothing", on, err)
	}
	// A person clears it: the administrator (the control plane's ops login, EXE-06, uses this path).
	if _, err := admin.Exec(ctx(t), `UPDATE tenant SET kill_switch = false WHERE tenant_id = 'ten_a'`); err != nil {
		t.Fatalf("a person must be able to clear the kill switch: %v", err)
	}
}
