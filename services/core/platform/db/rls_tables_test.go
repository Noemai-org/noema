package db_test

// Ruling A6 (6 Nov 2026), REQ-SEC-004, INV-12, REQ-AUD-001, REQ-AUD-003: row-level security with FORCE on the Execution, Outcome and Record tables, keyed
// on the tenant of the login that connected (decision_login_tenant(), migration 0009). SR's cross-tenant read, write and update tests for these tables are
// noema#190; these tests pin the catalog (enabled AND forced, so the owner is subject too) and the one deliberate exception, the chain verifier.
// "Row security" restricts which rows a login sees; it does not authenticate a receipt or an event. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
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

// The Security Reviewer's condition on noema#229 (settled with C1 on that PR): receipt_insert takes p_tenant_id as an argument, and a SECURITY DEFINER function runs as its
// owner, so what refuses a tenant that is not the login's is row-level security with FORCE on execution_receipt (this migration). An execution login of tenant A calls
// receipt_insert with a genuine, validly signed receipt of tenant B and tenant B's own grant_spend: every check of the signature trigger passes, so only the policy can
// refuse it. The call is refused (NS001, because tenant B's spend is hidden from the definer's read, or 42501, the execution_receipt policy's WITH CHECK; the assertion accepts either), no execution_receipt row exists, and the same receipt is stored by tenant B's own login (the control: the
// refusal is the tenant, not the receipt). Mutation: policies that are USING (true) WITH CHECK (true) on grant_spend and execution_receipt let tenant A store it and fail this test.
func TestATenantAExecutionLoginCannotStoreAGenuineReceiptOfTenantB(t *testing.T) {
	w := newSigWorld(t) // ten_a, grt_1 and its spend; the session is mapped to ten_a
	k1 := w.register(t, "core-execution", 1)
	spend(t, w.pool, "ten_b", "grt_b", "nb", "idk_v1_b", 1)
	b := goodReceipt()
	b.tenant, b.grant, b.id, b.idk = "ten_b", "grt_b", "rcp_b", "idk_v1_b"
	args := w.call(t, b, k1, "")
	count := func() int {
		var n int
		if err := w.pool.QueryRow(ctx(t), `SELECT count(*) FROM execution_receipt WHERE receipt_id = 'rcp_b'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	loginA := testpg.TenantLogin(t, w.pool, w.dsn, "execution_rw", "ten_a")
	_, err := loginA.Exec(ctx(t), insertSQL, args...)
	// Refused by row security, at the first place it bites: the signature trigger reads tenant B's grant_spend as the definer, which FORCE subjects to the policy, so tenant A's
	// login does not see the spend and the receipt "does not match the recorded spend" (NS001); a build in which that read were allowed would meet the WITH CHECK of the
	// execution_receipt policy (42501). Either way the call is refused and no row is stored.
	if err == nil {
		t.Fatal("tenant A's execution login stored a genuine receipt of tenant B")
	}
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || (pe.Code != "NS001" && pe.Code != "42501") {
		t.Fatalf("tenant A's execution login storing a genuine receipt of tenant B: got %v, want NS001 (the spend is hidden) or 42501 (row-level security)", err)
	}
	if n := count(); n != 0 {
		t.Fatalf("%d receipt rows of tenant B exist after tenant A's refused call", n)
	}
	loginB := testpg.TenantLogin(t, w.pool, w.dsn, "execution_rw", "ten_b")
	if _, err := loginB.Exec(ctx(t), insertSQL, args...); err != nil {
		t.Fatalf("control: tenant B's own login must store its genuine receipt: %v", err)
	}
	if n := count(); n != 1 {
		t.Fatalf("control: %d receipt rows of tenant B, want 1", n)
	}
}
