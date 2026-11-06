package db_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DEBT-01 (Tier 0), SEC-13, REQ-AUD-006, REQ-EXE-004, INV-03, INV-17. A caller that holds only the execution_rw credential
// and bypasses the Controller must not be able to void a spent grant with a receipt it forged.
//
// AUTHENTICATION is what this file tests: a receipt reaches execution_receipt only if its signature verifies against a
// trusted public key. It is not a test that the role is restricted: restricting INSERT would stop the Controller's own
// writes and would not authenticate anything (the evaluation, docs/security/w2-ed25519-extension-evaluation.md section 3).
// Today the guard of migration 0002 checks only the shape of the signature, and the database holds no key, so these tests
// FAIL on the held noema#94 code (the forged receipt is accepted). They are expected to pass when W4 (EXE-08) lands.
//
// WHAT COUNTS AS REFUSED (changed 6 Nov 2026). "Any insert error" was not enough: a bare REVOKE INSERT ON execution_receipt
// FROM execution_rw would have passed it, and that is restriction, not authentication. A forged receipt must now be refused
// with SQLSTATE NS001, the code DEC-2026-11-01 D12 gives the trigger for "signature not verified". A privilege error (42501),
// a missing column (42703), any other code, or an accepted receipt fails the test.
// A forgery in the old HMAC shape (64 hex) is a malformed Ed25519 signature after W4. The verifier may refuse it as NS001 or
// as SG001 (the spike's code for a signature that is not 64 bytes); both are the signature layer and nothing else is.
// The positive half (a receipt signed by a trusted key IS accepted on the legitimate path) needs W4's key registration and is
// the next SEC-13 test; until it exists a pass here is not evidence that DEBT-01 is closed.
//
// ASSUMPTION about W4 (DEC-2026-11-01 D8): the signed bytes are stored in a column signed_jcs. When the column exists the
// forgery supplies it, so the forgery is complete in every respect except the signature. If W4 names or shapes it differently,
// this helper changes; the test then fails loudly (a different SQLSTATE), never silently.
//
// Every statement here is run as execution_rw, in one transaction, with no Controller code. Synthetic data only.
//
// The helpers rcp, refusal, spentGrant and status come from grant_void_test.go (noema#94); this file does not compile without it.

const (
	sigNotVerified = "NS001" // DEC-2026-11-01 D12: signature not verified
	sigWrongLength = "SG001" // spike wrapper: signature or key of the wrong length
	sigDeniedByACL = "42501" // insufficient_privilege: restriction, not authentication
)

// forgery is one receipt an attacker could write. Each one is complete: every column and every member of the signed
// document agrees with the spend, the state is REJECTED_STALE_VERSION and the class is VERSION_CONFLICT_409_412. Only the
// signature is unauthentic.
type forgery struct {
	name string
	make func() rcp
	// wantCodes are the SQLSTATEs that count as a refusal by the signature layer once W4 has landed.
	wantCodes []string
}

func forgeries() []forgery {
	with := func(key, value string) func() rcp {
		return func() rcp { r := refusal("grt_1"); r.sigKey, r.sigVal = key, value; return r }
	}
	strict := []string{sigNotVerified}
	legacy := []string{sigNotVerified, sigWrongLength}
	return []forgery{
		// Ed25519 shape (DEC-2026-11-01 D4): a key id of the right form and a 128-hex value. Well formed, unauthentic.
		{"Ed25519 shape, key id of a key that was never registered", with("core-execution.ed25519.9", strings.Repeat("ab", 64)), strict},
		{"Ed25519 shape, key id of another identity", with("attacker.ed25519.1", strings.Repeat("cd", 64)), strict},
		{"Ed25519 shape, all-zero signature", with("core-execution.ed25519.1", strings.Repeat("00", 64)), strict},
		// The old HMAC shape, the only one noema#94's guard accepts: this is the forgery that voids a grant on the held code.
		{"HMAC shape, 64 hex that verifies against nothing, under a key id the core uses", with("k1", strings.Repeat("ab", 32)), legacy},
		{"HMAC shape, another 64 hex value, under a key id that does not exist", with("k_attacker", strings.Repeat("cd", 32)), legacy},
		{"HMAC shape, the all-zero value", with("k1", strings.Repeat("00", 32)), legacy},
	}
}

// sqlState returns the SQLSTATE of a database error, or "" for an error that is not one.
func sqlState(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// forgedVoid runs the DEBT-01 sequence as execution_rw in one transaction and reports what the database let through.
func forgedVoid(t *testing.T, f forgery) (insertErr, voidErr error, finalStatus string) {
	t.Helper()
	pool := migrated(t)
	seed(t, pool)
	spentGrant(t, pool, "grt_1", "n1")
	r := f.make()
	doc := r.document()
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := map[string]any{}
	for k, v := range doc {
		if k != "sig" {
			unsigned[k] = v
		}
	}
	// encoding/json writes object members in key order and these members are ASCII strings and integers, which is what RFC 8785
	// (JCS) gives for them, so these are the bytes W4's signer would have signed.
	signed, err := json.Marshal(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	var hasSignedJCS bool
	if err := pool.QueryRow(ctx(t), `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'execution_receipt' AND column_name = 'signed_jcs')`).Scan(&hasSignedJCS); err != nil {
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
	cols := `receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, external_ack, dispatched_at, json, sig_key_id, sig_value, created_at`
	vals := `$1, $2, $3, $4, $5, 'ROUTE_CASE', $6, $7, $8, now(), $9::jsonb, $10, $11, now()`
	args := []any{fmt.Sprintf("rcp_%s_%d", r.grant, r.attempt), r.tenant, r.grant, r.attempt, r.key, r.state, r.raw, r.ack, string(body), r.sigKey, r.sigVal}
	if hasSignedJCS {
		cols += ", signed_jcs"
		vals += ", $12"
		args = append(args, signed)
	}
	_, insertErr = tx.Exec(ctx(t), `INSERT INTO execution_receipt (`+cols+`) VALUES (`+vals+`)`, args...)
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

// DEBT-01 closure criterion 3: a caller with execution_rw cannot void a grant with a forged well-formed receipt, by INSERT,
// and the database says why: the signature did not verify.
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
			got := sqlState(insertErr)
			if got == sigDeniedByACL {
				t.Fatalf("the insert was refused by a privilege (%v): that is restriction, not authentication; the receipt must reach the signature check and fail it", insertErr)
			}
			for _, want := range f.wantCodes {
				if got == want {
					return
				}
			}
			t.Fatalf("the insert was refused with SQLSTATE %q (%v), want one of %v: the refusal must come from the signature check", got, insertErr, f.wantCodes)
		})
	}
}
