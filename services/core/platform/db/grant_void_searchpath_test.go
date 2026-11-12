package db_test

// SR's finding on noema#233 (and noema#251): decision_grant_guard reads execution_receipt and grant_spend by unqualified name as the role that issued the UPDATE, so a
// session that may create a temporary table can shadow execution_receipt with a forged "confirmed refusal" and void a spent grant with no signature check at all: the
// DEBT-01 bypass through the search path instead of through the insert. REQ-AUD-006, REQ-EXE-004, INV-03, INV-04. All rows are SYNTHETIC.

import (
	"encoding/json"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

func TestATemporaryReceiptTableCannotVoidASpentGrant(t *testing.T) {
	admin, dsn := migratedDSN(t)
	spend(t, admin, "ten_a", "grt_tmp", "n_tmp", "idk_v1_grt_tmp", 1) // a SPENT grant with its spend, and no receipt at all
	exec := testpg.TenantLogin(t, admin, dsn, "execution_rw", "ten_a")
	conn, err := exec.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx(t), `CREATE TEMP TABLE execution_receipt (LIKE public.execution_receipt)`); err != nil {
		t.Fatalf("the test cluster must let the login create a temporary table (the PUBLIC TEMPORARY privilege): %v", err)
	}
	r := refusal("grt_tmp")
	body, err := json.Marshal(r.document())
	if err != nil {
		t.Fatal(err)
	}
	// A forged, unsigned row in the temporary table that satisfies every condition of the permitted transition.
	if _, err := conn.Exec(ctx(t), `INSERT INTO pg_temp.execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, external_ack, dispatched_at, json, sig_key_id, sig_value, signed_jcs, created_at)
		VALUES ('rcp_tmp', $1, $2, $3, $4, 'ROUTE_CASE', $5, $6, $7, now(), $8::jsonb, 'k1', 'aa', '\x00'::bytea, now())`,
		r.tenant, r.grant, r.attempt, r.key, r.state, r.raw, r.ack, string(body)); err != nil {
		t.Fatalf("plant the forged temporary row: %v", err)
	}
	_, err = conn.Exec(ctx(t), `UPDATE decision_grant SET status = 'VOID' WHERE grant_id = 'grt_tmp'`)
	wantCode(t, err, violation, "voiding a spent grant on the strength of a temporary execution_receipt")
	if got := status(t, admin, "grt_tmp"); got != "SPENT" {
		t.Fatalf("the grant is %s after a void backed only by a temporary table; it must stay SPENT", got)
	}
}
