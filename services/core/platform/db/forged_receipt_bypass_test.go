package db_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// DEBT-01 (Tier 0), SEC-13, REQ-AUD-006, REQ-EXE-004, INV-03, INV-17. A caller that holds only the execution_rw credential
// and bypasses the Controller must not be able to void a spent grant with a receipt it forged.
//
// AUTHENTICATION is what this file tests: a receipt reaches execution_receipt only if its signature verifies against a
// trusted public key. It is not a test that the role is restricted: restricting INSERT would stop the Controller's own
// writes and would not authenticate anything (the evaluation, docs/security/w2-ed25519-extension-evaluation.md section 3).
// The two controls have separate tests. Today the guard of migration 0002 checks only the shape of the signature, and the
// database holds no key, so these tests FAIL on the held noema#94 code. They are expected to pass when W4 (EXE-08) lands.
//
// KNOWN LIMIT: this test is satisfied by any refusal, so a bare REVOKE INSERT ON execution_receipt FROM execution_rw would
// pass it (checked on 1 Nov 2026 with a throwaway migration, since removed). That is restriction, not authentication. It
// closes DEBT-01 only together with the positive test SEC-13 adds once GOV-28 fixes the signature format: a receipt signed
// with a trusted key IS accepted on the legitimate write path, and a receipt with a wrong, replayed, revoked or unknown key
// is not. Until that test exists, a pass here is not evidence that DEBT-01 is closed.
//
// Every statement here is run as execution_rw, in one transaction, with no Controller code. Synthetic data only.
//
// The helpers rcp, refusal, spentGrant and status come from grant_void_test.go (noema#94); this file does not compile without it.

// forgery is one receipt an attacker could write. Each one is complete: every column and every member of the signed
// document agrees with the spend, the state is REJECTED_STALE_VERSION and the class is VERSION_CONFLICT_409_412. Only the
// signature is unauthentic.
type forgery struct {
	name string
	make func() rcp
}

func forgeries() []forgery {
	with := func(key, value string) func() rcp {
		return func() rcp { r := refusal("grt_1"); r.sigKey, r.sigVal = key, value; return r }
	}
	return []forgery{
		{"64 lower-case hex that verifies against nothing, under a key id the core uses", with("k1", strings.Repeat("ab", 32))},
		{"another 64 lower-case hex value, under a key id that does not exist", with("k_attacker", strings.Repeat("cd", 32))},
		{"the all-zero hex value", with("k1", strings.Repeat("00", 32))},
	}
}

// forgedVoid runs the DEBT-01 sequence as execution_rw in one transaction and reports what the database let through.
func forgedVoid(t *testing.T, f forgery) (insertErr, voidErr error, finalStatus string) {
	t.Helper()
	pool := migrated(t)
	seed(t, pool)
	spentGrant(t, pool, "grt_1", "n1")
	r := f.make()
	body, err := json.Marshal(r.document())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := tx.Exec(ctx(t), "SET LOCAL ROLE "+pgx.Identifier{"execution_rw"}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	// A savepoint per step keeps the transaction usable after a refusal, so the second step is still tried.
	if _, err := tx.Exec(ctx(t), "SAVEPOINT s1"); err != nil {
		t.Fatal(err)
	}
	_, insertErr = tx.Exec(ctx(t), `INSERT INTO execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, external_ack, dispatched_at, json, sig_key_id, sig_value, created_at)
		VALUES ($1, $2, $3, $4, $5, 'ROUTE_CASE', $6, $7, $8, now(), $9::jsonb, $10, $11, now())`,
		fmt.Sprintf("rcp_%s_%d", r.grant, r.attempt), r.tenant, r.grant, r.attempt, r.key, r.state, r.raw, r.ack, string(body), r.sigKey, r.sigVal)
	if insertErr != nil {
		if _, err := tx.Exec(ctx(t), "ROLLBACK TO SAVEPOINT s1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx(t), "SAVEPOINT s2"); err != nil {
		t.Fatal(err)
	}
	_, voidErr = tx.Exec(ctx(t), voidGrant1)
	if voidErr != nil {
		if _, err := tx.Exec(ctx(t), "ROLLBACK TO SAVEPOINT s2"); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.QueryRow(ctx(t), `SELECT status FROM decision_grant WHERE grant_id = 'grt_1'`).Scan(&finalStatus); err != nil {
		t.Fatal(err)
	}
	return insertErr, voidErr, finalStatus
}

// DEBT-01 closure criterion 3: a caller with execution_rw cannot void a grant with a forged well-formed receipt, by INSERT.
func TestExecutionRWCannotVoidASpentGrantWithAForgedReceipt(t *testing.T) {
	for _, f := range forgeries() {
		t.Run(f.name, func(t *testing.T) {
			insertErr, voidErr, finalStatus := forgedVoid(t, f)
			if finalStatus == "VOID" {
				t.Fatalf("DEBT-01 BYPASS: execution_rw inserted a forged receipt (insert error: %v) and voided a SPENT grant (void error: %v); "+
					"the database authenticated nothing", insertErr, voidErr)
			}
			if finalStatus != "SPENT" {
				t.Fatalf("status %q, want SPENT", finalStatus)
			}
			if insertErr == nil {
				t.Fatalf("the forged receipt was accepted into execution_receipt (the grant stayed SPENT only because the void guard refused something else): " +
					"a receipt must be refused at insert when its signature does not verify")
			}
		})
	}
}
