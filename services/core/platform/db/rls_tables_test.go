package db_test

// Ruling A6 (6 Nov 2026), REQ-SEC-004, INV-12, REQ-AUD-001, REQ-AUD-003: row-level security with FORCE on the Execution, Outcome and Record tables, keyed
// on the tenant of the login that connected (decision_login_tenant(), migration 0009). SR's cross-tenant read, write and update tests for these tables are
// noema#190; these tests pin the catalog (enabled AND forced, so the owner is subject too) and the one deliberate exception, the chain verifier.
// "Row security" restricts which rows a login sees; it does not authenticate a receipt or an event. All data is SYNTHETIC.

import (
	"testing"
)

var rlsTables = []string{"grant_spend", "idempotency_record", "execution_receipt", "recovery_request", "outbox_event", "outcome_verdict", "outcome_watch",
	"record_event", "record_chain_head"}

func TestRowSecurityIsEnabledAndForcedOnTheExecutionOutcomeAndRecordTables(t *testing.T) {
	pool := migrated(t)
	for _, tbl := range rlsTables {
		var enabled, forced bool
		if err := pool.QueryRow(ctx(t), `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = ('public.' || $1)::regclass`, tbl).Scan(&enabled, &forced); err != nil {
			t.Fatalf("%s: %v", tbl, err)
		}
		if !enabled || !forced {
			t.Errorf("%s: row security enabled=%v forced=%v, want both", tbl, enabled, forced)
		}
	}
}

// Every policy on these tables is tied to the login's tenant, except the verifier's read-only one: no policy is USING (true) for a role that can write.
func TestNoPolicyOnTheseTablesLetsAWritingRoleSeeEveryTenant(t *testing.T) {
	pool := migrated(t)
	rows, err := pool.Query(ctx(t), `SELECT tablename, policyname, cmd, coalesce(qual, ''), roles::text FROM pg_policies WHERE schemaname = 'public' AND tablename = ANY($1) ORDER BY 1, 2`, rlsTables)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var tbl, pol, cmd, qual, roles string
		if err := rows.Scan(&tbl, &pol, &cmd, &qual, &roles); err != nil {
			t.Fatal(err)
		}
		n++
		if qual == "true" && !(cmd == "SELECT" && roles == "{record_verify}") {
			t.Errorf("%s.%s: USING (true) for %s on %s: only the verifier's SELECT may see every tenant", tbl, pol, roles, cmd)
		}
	}
	if n == 0 {
		t.Fatal("no policy found on any of the tables: row security is not in place")
	}
}

// The chain verifier (REC-05) reads every tenant's chain head and writes nothing, by policy and by privilege.
func TestTheVerifierReadsEveryTenantsChainHeadAndWritesNothing(t *testing.T) {
	pool := migrated(t)
	for _, q := range []string{
		`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A'), ('ten_b', 'B')`,
		`INSERT INTO record_chain_head (tenant_id, seq, hash) VALUES ('ten_a', 0, 'sha256:a'), ('ten_b', 0, 'sha256:b')`,
	} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	var n int
	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := tx.Exec(ctx(t), `SET LOCAL ROLE record_verify`); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx(t), `SELECT count(*) FROM record_chain_head`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("record_verify sees %d chain heads (%v), want both tenants'", n, err)
	}
	if _, err := tx.Exec(ctx(t), `SAVEPOINT s`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx(t), `UPDATE record_chain_head SET seq = 9`); err == nil {
		t.Error("record_verify updated a chain head")
	}
}
