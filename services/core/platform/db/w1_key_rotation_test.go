package db_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SEC-09 and DEBT-01 closure criterion (4), REQ-SEC-005, REQ-AUD-006. The rotation drill for the Ed25519 receipt keys (W1 D5, test 10): at
// most two keys of an identity are ACTIVE; during the overlap a receipt under the old key and one under the new key both insert; after the
// old key is retired it is refused for new inserts; a revoked key is refused; a third ACTIVE key cannot be registered. SYNTHETIC keys.
// FAILS LOUDLY until W4 (EXE-08) lands: it does not skip. Names marked ADAPTER in w1_receipt_matrix_test.go are the ones to adjust.
//
// What this does not show, and where it is shown: that a retired key's old objects still verify in the chain verifier and that objects
// at or after compromised_since are reported untrusted (REC-05 and REC-08, the chain verifier), and the grant-key half of the drill
// (noema#226, trust/grants).

// receiptFor builds a receipt for grant (already SPENT by spentGrant) signed by k.
func receiptFor(t *testing.T, k w1Key, grant string) (rcp, []byte, string, string) {
	t.Helper()
	return signedReceipt(t, k, w1TagReceipt, func(r *rcp) {
		r.grant = grant
		r.key = "idk_v1_" + grant
	})
}

// insertFor inserts one receipt and returns the error.
func insertFor(t *testing.T, pool *pgxpool.Pool, k w1Key, grant string) error {
	t.Helper()
	r, jcs, keyID, sig := receiptFor(t, k, grant)
	return receiptRow(t, pool, r, jcs, keyID, sig)
}

func TestW1ReceiptKeyRotationOverlapRetirementAndRevocation(t *testing.T) {
	pool := migrated(t)
	requireW4(t, pool)
	seed(t, pool)
	for i, g := range []string{"grt_1", "grt_2", "grt_3", "grt_4", "grt_5"} {
		spentGrant(t, pool, g, "n"+string(rune('1'+i)))
	}
	k1 := newW1Key("core-execution", 1)
	k2 := newW1Key("core-execution", 2)
	k3 := newW1Key("core-execution", 3)

	registerKey(t, pool, k1)
	if err := insertFor(t, pool, k1, "grt_1"); err != nil {
		t.Fatalf("a receipt under the only ACTIVE key was refused: %v", err)
	}

	// Overlap: both keys ACTIVE. The signer would use the newest, but the database accepts either.
	registerKey(t, pool, k2)
	if err := insertFor(t, pool, k2, "grt_2"); err != nil {
		t.Fatalf("overlap: a receipt under the NEW key was refused: %v", err)
	}
	if err := insertFor(t, pool, k1, "grt_3"); err != nil {
		t.Fatalf("overlap: a receipt under the OLD key was refused: %v", err)
	}

	// A third ACTIVE key of the same identity cannot be registered (at most two: D5).
	if _, err := pool.Exec(ctx(t), adapterRegister, k3.identity, k3.version, k3.public()); err == nil {
		t.Fatal("a third ACTIVE key of one identity was registered; the overlap would never end")
	}
	if err := insertFor(t, pool, k3, "grt_4"); err == nil {
		t.Fatal("a receipt under a key that was never registered was accepted")
	}

	// Retirement: the old key is refused for NEW inserts; the new key still works.
	callKey(t, pool, adapterRetire, k1.keyID())
	if err := insertFor(t, pool, k1, "grt_4"); err == nil {
		t.Fatal("a RETIRED key was accepted for a new insert")
	}
	if err := insertFor(t, pool, k2, "grt_4"); err != nil {
		t.Fatalf("after retirement of the old key, a receipt under the new key was refused: %v", err)
	}
	// The receipts written under the old key are still there: retirement does not delete or rewrite evidence.
	var n int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM execution_receipt WHERE sig_key_id = $1`, k1.keyID()).Scan(&n); err != nil || n != 2 {
		t.Fatalf("receipts under the retired key: %d (err %v), want 2", n, err)
	}

	// Revocation: a compromised key is refused for new inserts too.
	callKey(t, pool, adapterRevoke, k2.keyID())
	if err := insertFor(t, pool, k2, "grt_5"); err == nil {
		t.Fatal("a REVOKED key was accepted for a new insert")
	}
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM execution_receipt`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("receipt rows: %d (err %v), want 4 (grt_1 to grt_4)", n, err)
	}
}

// A rotation cannot be driven by a runtime role: execution_rw cannot register, retire or revoke a key (D5; W1 test 7).
func TestW1RuntimeRolesCannotRotateKeys(t *testing.T) {
	pool := migrated(t)
	requireW4(t, pool)
	k := newW1Key("core-execution", 1)
	registerKey(t, pool, k)
	attempts := map[string][]any{
		adapterRegister: {"core-execution", 2, newW1Key("core-execution", 2).public()},
		adapterRetire:   {k.keyID()},
		adapterRevoke:   {k.keyID()},
	}
	for _, role := range []string{"execution_rw", "trust_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "record_append"} {
		for sql, args := range attempts {
			tx, err := pool.Begin(ctx(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx(t), `SET LOCAL ROLE `+role); err != nil {
				_ = tx.Rollback(ctx(t))
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx(t), sql, args...)
			_ = tx.Rollback(ctx(t))
			if err == nil {
				t.Errorf("%s ran %s (ADAPTER): only the migration role may rotate keys", role, sql)
			}
		}
	}
}

// DEBT-01 closure check 1 (replay), the case the matrix does not cover: the SAME genuine receipt, byte for byte, inserted twice. The
// second insert is refused (UNIQUE (grant_id, attempt), W1 D7) and only one row exists.
func TestW1AGenuineReceiptCannotBeInsertedTwice(t *testing.T) {
	pool := migrated(t)
	requireW4(t, pool)
	seed(t, pool)
	spentGrant(t, pool, "grt_1", "n1")
	k := newW1Key("core-execution", 1)
	registerKey(t, pool, k)
	r, jcs, keyID, sig := receiptFor(t, k, "grt_1")
	if err := receiptRow(t, pool, r, jcs, keyID, sig); err != nil {
		t.Fatalf("the genuine receipt was refused (the positive control): %v", err)
	}
	if err := receiptRow(t, pool, r, jcs, keyID, sig); err == nil {
		t.Fatal("the same genuine receipt was accepted twice")
	}
	var n int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM execution_receipt`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d receipt rows (err %v), want 1", n, err)
	}
}
