package db_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

// Error classes the database must answer with: 42501 is a missing privilege, class 23 is a violated constraint
// or an immutability trigger.
const (
	denied    = "42501"
	violation = "23"
)

func migrated(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _ := migratedDSN(t)
	return pool
}

func migratedDSN(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	pool, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool, dsn
}

func seed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`,
		`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		 VALUES ('dec_1', 'ten_a', '{"decision_id":"dec_1","tenant_id":"ten_a","outcome":"ALLOW","sig":{"key_id":"k1","value":"aa"}}', 'ALLOW', 'sha256:' || repeat('0', 64), '[]', 'k1', 'aa')`,
		`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		 VALUES ('grt_1', 'ten_a', 'dec_1', 'n1', '{"decision_id":"dec_1","tenant_id":"ten_a"}', 'pol_1', '1', now() + interval '1 hour', 'k1', 'aa')`,
	} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// try runs one statement as a role inside a transaction that is always rolled back, and returns its error.
func try(t *testing.T, pool *pgxpool.Pool, role, stmt string, args ...any) error {
	t.Helper()
	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := tx.Exec(ctx(t), "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx(t), stmt, args...)
	return err
}

func wantCode(t *testing.T, err error, prefix, what string) {
	t.Helper()
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || len(pe.Code) < len(prefix) || pe.Code[:len(prefix)] != prefix {
		t.Fatalf("%s: got %v, want an error with code %s*", what, err, prefix)
	}
}

// REQ-DATA-001 acceptance: execution_rw cannot write decision_record's neighbour decision_grant, outcome_rw
// cannot write execution_receipt, and so on. The matrix reads the privileges the database holds.
func TestRolePermissionMatrix(t *testing.T) {
	pool := migrated(t)
	type cell struct {
		role, table, priv string
		want              bool
	}
	cells := []cell{
		{"trust_rw", "decision_grant", "INSERT", true},
		{"trust_rw", "grant_spend", "INSERT", false},
		{"trust_rw", "execution_receipt", "INSERT", false},
		{"execution_rw", "decision_grant", "INSERT", false},
		{"execution_rw", "decision_grant", "DELETE", false},
		{"execution_rw", "grant_spend", "INSERT", true},
		{"execution_rw", "execution_receipt", "INSERT", true},
		{"execution_rw", "idempotency_record", "INSERT", true},
		{"execution_rw", "recovery_request", "INSERT", true},
		{"outcome_rw", "execution_receipt", "INSERT", false},
		{"outcome_rw", "grant_spend", "INSERT", false},
		{"outcome_rw", "decision_grant", "INSERT", false},
		{"outcome_rw", "execution_receipt", "SELECT", true},
		{"evidence_rw", "execution_receipt", "SELECT", false},
		{"orchestrator_rw", "execution_receipt", "INSERT", false},
	}
	for _, c := range cells {
		var got bool
		if err := pool.QueryRow(ctx(t), `SELECT has_table_privilege($1, $2, $3)`, c.role, c.table, c.priv).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s %s on %s = %v, want %v", c.role, c.priv, c.table, got, c.want)
		}
	}
	// No power has any privilege on the Record tables; they reach them only through the two functions.
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw"} {
		for _, table := range []string{"record_event", "record_chain_head"} {
			for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
				var got bool
				if err := pool.QueryRow(ctx(t), `SELECT has_table_privilege($1, $2, $3)`, role, table, priv).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got {
					t.Errorf("%s holds %s on %s", role, priv, table)
				}
			}
		}
	}
}

// REQ-AUD-001 acceptance: runtime credentials attempting UPDATE or DELETE on the Record are denied.
func TestPowersCannotTouchTheRecordDirectly(t *testing.T) {
	pool := migrated(t)
	seed(t, pool)
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw"} {
		for _, stmt := range []string{
			`INSERT INTO record_event (event_id, tenant_id, seq, prev_hash, hash, written_by, event_type, object_ref, payload_hash, correlation_id, at, schema_version, actor, body)
			 VALUES ('recev_x', 'ten_a', 1, 'p', 'h', 'w', 'T', 'o', 'ph', 'c', now(), '1', 'a', '{}')`,
			`UPDATE record_event SET event_type = 'X'`,
			`DELETE FROM record_event`,
			`TRUNCATE record_event`,
			`UPDATE record_chain_head SET seq = 99`,
		} {
			wantCode(t, try(t, pool, role, stmt), denied, role+": "+stmt)
		}
	}
}

// REQ-DATA-001 acceptance: immutability triggers reject UPDATE and DELETE on every insert-only table, even for
// the owner of the table.
func TestImmutabilityTriggersRejectUpdateDeleteAndTruncate(t *testing.T) {
	pool := migrated(t)
	seed(t, pool)
	for _, q := range []string{
		`INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ('grt_1', 'ten_a', 'n1', 'idk_v1_x', 1, now())`,
		`INSERT INTO execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, dispatched_at, json, sig_key_id, sig_value, created_at)
		 VALUES ('rcp_1', 'ten_a', 'grt_1', 1, 'idk_v1_x', 'A', 'OUTCOME_UNKNOWN', 'TIMEOUT', now(), '{}', 'k', 's', now())`,
		`INSERT INTO record_chain_head (tenant_id, seq, hash) VALUES ('ten_a', 0, 'sha256:0000000000000000000000000000000000000000000000000000000000000000')`,
	} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
	for _, c := range []struct{ table, update string }{
		{"grant_spend", `UPDATE grant_spend SET nonce = 'n2'`},
		{"execution_receipt", `UPDATE execution_receipt SET raw_status = 'x'`},
	} {
		wantCode(t, try(t, pool, "postgres", c.update), violation, c.table+" UPDATE")
		wantCode(t, try(t, pool, "postgres", `DELETE FROM `+c.table), violation, c.table+" DELETE")
		// A table other tables reference cannot be truncated at all (0A000); the trigger covers the others.
		if err := try(t, pool, "postgres", `TRUNCATE `+c.table); err == nil {
			t.Fatalf("%s TRUNCATE succeeded", c.table)
		}
	}
	// execution_receipt is referenced by outcome_verdict (0007), so it cannot be truncated at all (0A000, asserted above);
	// its TRUNCATE trigger must still exist for the day a reference goes away.
	var triggers int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM pg_trigger WHERE tgrelid = 'execution_receipt'::regclass AND tgname = 'execution_receipt_no_truncate' AND NOT tgisinternal`).Scan(&triggers); err != nil || triggers != 1 {
		t.Fatalf("execution_receipt TRUNCATE trigger: %d found, %v", triggers, err)
	}
	wantCode(t, try(t, pool, "postgres", `DELETE FROM decision_grant`), violation, "decision_grant DELETE")
	if err := try(t, pool, "postgres", `TRUNCATE decision_grant`); err == nil {
		t.Fatal("decision_grant TRUNCATE succeeded")
	}
}

// A grant's contract object never changes; only its status moves, once, to a final state (INV-03).
func TestDecisionGrantOnlyItsStatusMovesAndFinalIsFinal(t *testing.T) {
	pool := migrated(t)
	seed(t, pool)
	wantCode(t, try(t, pool, "postgres", `UPDATE decision_grant SET json = '{"a":1}'`), violation, "grant json")
	wantCode(t, try(t, pool, "postgres", `UPDATE decision_grant SET nonce = 'other'`), violation, "grant nonce")
	// execution_rw may set the status, and only the status.
	if err := try(t, pool, "execution_rw", `UPDATE decision_grant SET status = 'SPENT'`); err != nil {
		t.Fatalf("execution_rw status update: %v", err)
	}
	wantCode(t, try(t, pool, "execution_rw", `UPDATE decision_grant SET json = '{}'`), denied, "execution_rw grant json")
	wantCode(t, try(t, pool, "execution_rw", `UPDATE decision_grant SET policy_version = '9'`), denied, "execution_rw grant policy_version")

	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := tx.Exec(ctx(t), `UPDATE decision_grant SET status = 'VOID'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx(t), `UPDATE decision_grant SET status = 'ISSUED'`); err == nil {
		t.Fatal("a VOID grant went back to ISSUED")
	}
}

// Single use is a unique constraint on the grant ID and on the nonce, never workflow history (INV-30, REQ-EXE-001).
func TestGrantSpendIsUniqueOnGrantAndNonce(t *testing.T) {
	pool, dsn := migratedDSN(t)
	seed(t, pool)
	exec := testpg.TenantLogin(t, pool, dsn, "execution_rw", "ten_a") // the tenant's own login: row-level security (migration 0018) needs a mapped login
	if _, err := pool.Exec(ctx(t), `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		VALUES ('grt_2', 'ten_a', 'dec_1', 'n1', '{"decision_id":"dec_1","tenant_id":"ten_a"}', 'pol_1', '1', now() + interval '1 hour', 'k1', 'aa')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx(t), `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ('grt_1', 'ten_a', 'n1', 'idk_v1_x', 1, now())`); err != nil {
		t.Fatal(err)
	}
	_, err := exec.Exec(ctx(t), `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ('grt_1', 'ten_a', 'n9', 'idk_v1_x', 1, now())`)
	wantCode(t, err, violation, "second spend of one grant")
	_, err = exec.Exec(ctx(t), `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ('grt_2', 'ten_a', 'n1', 'idk_v1_x', 1, now())`)
	wantCode(t, err, violation, "second spend with one nonce")
	// The nonce is unique across tenants, not only within one (PERD 38.2: nonce UNIQUE).
	for _, q := range []string{
		`INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`,
		`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		 VALUES ('dec_9', 'ten_b', '{"decision_id":"dec_9","tenant_id":"ten_b","outcome":"ALLOW","sig":{"key_id":"k1","value":"aa"}}', 'ALLOW', 'sha256:' || repeat('0', 64), '[]', 'k1', 'aa')`,
		`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		 VALUES ('grt_3', 'ten_b', 'dec_9', 'n1', '{"decision_id":"dec_9","tenant_id":"ten_b"}', 'pol_1', '1', now() + interval '1 hour', 'k1', 'aa')`,
	} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	execB := testpg.TenantLogin(t, pool, dsn, "execution_rw", "ten_b") // ten_b's own login, so the refusal is the unique nonce and not the tenant's row security
	_, err = execB.Exec(ctx(t), `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ('grt_3', 'ten_b', 'n1', 'idk_v1_x', 1, now())`)
	wantCode(t, err, violation, "the same nonce under another tenant")
}

// The idempotency record keeps its key, action and parameters, and its attempt only moves forward (REQ-EXE-002).
func TestIdempotencyRecordGuard(t *testing.T) {
	pool := migrated(t)
	seed(t, pool)
	for _, q := range []string{
		`INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ('grt_1', 'ten_a', 'n1', 'idk_v1_x', 1, now())`,
		`INSERT INTO idempotency_record (tenant_id, idempotency_key, logical_action, params_hash, attempt, status, last_grant_id, created_at, updated_at)
		 VALUES ('ten_a', 'idk_v1_x', 'A', 'ph', 2, 'DISPATCHING', 'grt_1', now(), now())`,
	} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	wantCode(t, try(t, pool, "postgres", `UPDATE idempotency_record SET params_hash = 'other'`), violation, "params_hash change")
	wantCode(t, try(t, pool, "postgres", `UPDATE idempotency_record SET attempt = 1`), violation, "attempt going back")
	wantCode(t, try(t, pool, "postgres", `DELETE FROM idempotency_record`), violation, "delete")
	wantCode(t, try(t, pool, "execution_rw", `UPDATE idempotency_record SET params_hash = 'other'`), denied, "execution_rw params_hash")
	if err := try(t, pool, "execution_rw", `UPDATE idempotency_record SET status = 'RECEIPTED', external_ref = 'case_1', updated_at = now()`); err != nil {
		t.Fatalf("execution_rw receipts the record: %v", err)
	}
}
