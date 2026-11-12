package db_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SEC-13, DEBT-01 closure criteria (2) to (5), REQ-AUD-006, REQ-EXE-004. The database half of the W1 design (DEC-2026-11-01, Proposed,
// docs#80): a receipt reaches execution_receipt only if its Ed25519 signature verifies under an ACTIVE key of the identity core-execution.
// SYNTHETIC test keys. FAILS LOUDLY until W4 (EXE-08) lands: it does not skip. Every statement marked ADAPTER names a W4 entry point as
// the W1 text describes it (D5, D8, D11, D12) and is to be adjusted to the landed SQL; the cases and the expectations are the part that
// is fixed.

const (
	adapterVerify   = `SELECT to_regprocedure('noema_sig.verify(text,text,text,bytea,text)') IS NOT NULL` // ADAPTER (D11)
	adapterRegister = `SELECT key_register($1, $2, $3)`                                         // ADAPTER (D5): identity, version, public key
	adapterRetire   = `SELECT key_retire($1)`                                                   // ADAPTER (D5): key_id
	adapterRevoke   = `SELECT key_revoke($1, now() - interval '1 hour')`                        // ADAPTER (D5): key_id, compromised_since
	signedJCSColumn = "signed_jcs"                                                                        // ADAPTER (D8)
	migratorRole    = "noema_migrator"                                                                    // D14
)

func requireW4(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var present bool
	if err := pool.QueryRow(ctx(t), adapterVerify).Scan(&present); err != nil {
		t.Fatalf("W4 probe: %v", err)
	}
	if !present {
		t.Fatalf("W4 (EXE-08) has not landed: noema_sig.verify is absent, so no receipt is authenticated. DEBT-01 is open; this matrix cannot pass until it is")
	}
}

// receiptRow inserts one execution_receipt row as the superuser (the trigger of D12 is ENABLE ALWAYS, so it fires for every role) with the
// signed bytes stored beside the document, as D8 requires. It returns the insert error.
func receiptRow(t *testing.T, pool *pgxpool.Pool, r rcp, signedJCS []byte, keyID, sigHex string) error {
	t.Helper()
	if _, err := pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T') ON CONFLICT DO NOTHING`, r.tenant); err != nil {
		t.Fatal(err)
	}
	body := string(signedJCS)
	_, err := pool.Exec(ctx(t), `INSERT INTO execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, external_ack, dispatched_at, json, `+signedJCSColumn+`, sig_key_id, sig_value, created_at)
		VALUES ($1, $2, $3, $4, $5, 'ROUTE_CASE', $6, $7, $8, now(), $9::jsonb, convert_to($9, 'UTF8'), $10, $11, now())`,
		fmt.Sprintf("rcp_%s_%d", r.grant, r.attempt), r.tenant, r.grant, r.attempt, r.key, r.state, r.raw, r.ack, body, keyID, sigHex)
	return err
}

// signedReceipt builds the receipt for a SPENT grant and signs its document without sig, as W1 D6 specifies.
func signedReceipt(t *testing.T, k w1Key, tag string, mut func(*rcp)) (rcp, []byte, string, string) {
	t.Helper()
	r := refusal("grt_1")
	if mut != nil {
		mut(&r)
	}
	doc := r.document()
	delete(doc, "sig")
	jcs, keyID, sigHex, err := signW1(k, tag, doc)
	if err != nil {
		t.Fatal(err)
	}
	return r, jcs, keyID, sigHex
}

func TestW1ReceiptVerificationMatrix(t *testing.T) {
	good := newW1Key("core-execution", 1)
	cases := []struct {
		name   string
		setup  func(t *testing.T, pool *pgxpool.Pool)
		build  func(t *testing.T) (rcp, []byte, string, string)
		accept bool
	}{
		{"a validly signed receipt under an ACTIVE key is accepted", nil, func(t *testing.T) (rcp, []byte, string, string) { return signedReceipt(t, good, w1TagReceipt, nil) }, true},
		{"signed by a key the database does not know", func(t *testing.T, p *pgxpool.Pool) {}, func(t *testing.T) (rcp, []byte, string, string) {
			return signedReceipt(t, newW1Key("core-execution", 9), w1TagReceipt, nil)
		}, false},
		{"signed by the wrong key under the right key id (wrong signature)", nil, func(t *testing.T) (rcp, []byte, string, string) {
			r, jcs, _, sig := signedReceipt(t, newW1Key("core-execution", 7), w1TagReceipt, nil)
			return r, jcs, good.keyID(), sig
		}, false},
		{"signed by core-outcome, which may not sign receipts (wrong identity)", func(t *testing.T, p *pgxpool.Pool) { registerKey(t, p, newW1Key("core-outcome", 1)) }, func(t *testing.T) (rcp, []byte, string, string) {
			return signedReceipt(t, newW1Key("core-outcome", 1), w1TagReceipt, nil)
		}, false},
		{"signed under the record-event tag (tag confusion)", nil, func(t *testing.T) (rcp, []byte, string, string) { return signedReceipt(t, good, w1TagEvent, nil) }, false},
		{"an HMAC-shaped 64-hex value under an Ed25519 key id (algorithm confusion, the DEBT-01 forgery)", nil, func(t *testing.T) (rcp, []byte, string, string) {
			r, jcs, keyID, _ := signedReceipt(t, good, w1TagReceipt, nil)
			return r, jcs, keyID, strings.Repeat("ab", 32)
		}, false},
		{"a 126-hex signature (not 128)", nil, func(t *testing.T) (rcp, []byte, string, string) {
			r, jcs, keyID, sig := signedReceipt(t, good, w1TagReceipt, nil)
			return r, jcs, keyID, sig[:126]
		}, false},
		{"a genuine signature replayed against another grant", nil, func(t *testing.T) (rcp, []byte, string, string) {
			r, jcs, keyID, sig := signedReceipt(t, good, w1TagReceipt, nil)
			r.grant = "grt_2" // the row says another grant; the signed bytes are the first grant's
			return r, jcs, keyID, sig
		}, false},
		{"a genuine signature replayed with another tenant, attempt or state in the row", nil, func(t *testing.T) (rcp, []byte, string, string) {
			r, jcs, keyID, sig := signedReceipt(t, good, w1TagReceipt, nil)
			r.state = "ACKNOWLEDGED"
			return r, jcs, keyID, sig
		}, false},
		{"signed_jcs that is not the document in the row", nil, func(t *testing.T) (rcp, []byte, string, string) {
			r, jcs, keyID, sig := signedReceipt(t, good, w1TagReceipt, nil)
			return r, []byte(strings.Replace(string(jcs), "ten_a", "ten_b", 1)), keyID, sig
		}, false},
		{"a RETIRED key is refused for a new insert", func(t *testing.T, p *pgxpool.Pool) { callKey(t, p, adapterRetire, good.keyID()) }, func(t *testing.T) (rcp, []byte, string, string) {
			return signedReceipt(t, good, w1TagReceipt, nil)
		}, false},
		{"a REVOKED key is refused for a new insert", func(t *testing.T, p *pgxpool.Pool) { callKey(t, p, adapterRevoke, good.keyID()) }, func(t *testing.T) (rcp, []byte, string, string) {
			return signedReceipt(t, good, w1TagReceipt, nil)
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool := migrated(t)
			requireW4(t, pool)
			seed(t, pool)
			spentGrant(t, pool, "grt_1", "n1")
			registerKey(t, pool, good)
			if c.setup != nil {
				c.setup(t, pool)
			}
			r, jcs, keyID, sig := c.build(t)
			err := receiptRow(t, pool, r, jcs, keyID, sig)
			if c.accept && err != nil {
				t.Fatalf("a validly signed receipt was refused (the positive control): %v", err)
			}
			if !c.accept && err == nil {
				t.Fatalf("accepted a receipt that must be refused (DEBT-01 closure criterion 3)")
			}
			var n int
			_ = pool.QueryRow(ctx(t), `SELECT count(*) FROM execution_receipt`).Scan(&n)
			if want := map[bool]int{true: 1, false: 0}[c.accept]; n != want {
				t.Fatalf("%d receipt rows, want %d", n, want)
			}
		})
	}
}

func registerKey(t *testing.T, pool *pgxpool.Pool, k w1Key) {
	t.Helper()
	if _, err := pool.Exec(ctx(t), adapterRegister, k.identity, k.version, k.public()); err != nil {
		t.Fatalf("register %s (ADAPTER): %v", k.keyID(), err)
	}
}

func callKey(t *testing.T, pool *pgxpool.Pool, sql, keyID string) {
	t.Helper()
	if _, err := pool.Exec(ctx(t), sql, keyID); err != nil {
		t.Fatalf("%s (ADAPTER): %v", sql, err)
	}
}

// DEBT-01 closure check "the migrator credential is absent from the core's runtime" (Architect, 1 Nov; W1 D14): no runtime role is a
// member of noema_migrator, directly or through another role, none owns a signed table or the key table, and none can create anything.
func TestW1NoRuntimeRoleIsTheMigrator(t *testing.T) {
	pool := migrated(t)
	requireW4(t, pool)
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "record_append"} {
		var member, create, dbCreate, super bool
		if err := pool.QueryRow(ctx(t), `SELECT pg_has_role($1, $2, 'member'), has_schema_privilege($1, 'public', 'CREATE'),
			has_database_privilege($1, current_database(), 'CREATE'), (SELECT rolsuper OR rolcreaterole OR rolcreatedb OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname = $1)`,
			role, migratorRole).Scan(&member, &create, &dbCreate, &super); err != nil {
			t.Fatal(err)
		}
		if member || create || dbCreate || super {
			t.Errorf("%s: member of %s=%v, CREATE on schema=%v, CREATE on database=%v, privileged attribute=%v", role, migratorRole, member, create, dbCreate, super)
		}
		var owned []string
		rows, err := pool.Query(ctx(t), `SELECT c.relname FROM pg_class c JOIN pg_roles r ON r.oid = c.relowner WHERE r.rolname = $1 AND c.relnamespace = 'public'::regnamespace`, role)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			owned = append(owned, n)
		}
		rows.Close()
		if len(owned) > 0 {
			t.Errorf("%s owns %v; after W4 the migrator owns the signed tables, and record_append holds grants, not ownership (D15, D16)", role, owned)
		}
	}
}
