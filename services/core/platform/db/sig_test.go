package db_test

// EXE-08 (W4) slice B, DEBT-01 Option B: the database verifies every execution receipt's Ed25519 signature before it is stored
// (DEC-2026-11-01 D4 to D8, D10 to D13, D15, D16; migration 0013). REQ-AUD-006, REQ-EXE-004, INV-04, INV-27. These tests need the pgsodium
// extension in the test cluster (testpg bootstraps it when it is installed; the CI job builds it in, ruling A16). A cluster without it
// fails them: there is no skip. Every key and document is SYNTHETIC (AGENT-25): generated in the test, never mounted anywhere.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

const (
	receiptTag = "noema/receipt/v1\n"
	// NS001 is the SQLSTATE of "signature not verified" (D12).
	ns001 = "NS001"
)

type sigWorld struct {
	pool *pgxpool.Pool
	dsn  string
	keys map[string]ed25519.PrivateKey // key id to private key; the database holds only the public halves
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

// register adds a public key as the deploy step does (key_register, D3a) and remembers the private half for signing.
func (w *sigWorld) register(t *testing.T, identity string, version int) string {
	t.Helper()
	priv := newKey(t)
	var id string
	if err := w.pool.QueryRow(ctx(t), `SELECT key_register($1, $2, $3)`, identity, version, []byte(priv.Public().(ed25519.PublicKey))).Scan(&id); err != nil {
		t.Fatalf("key_register(%s, %d): %v", identity, version, err)
	}
	w.keys[id] = priv
	return id
}

// spend seeds a grant and its spend (E8) for one tenant.
func spend(t *testing.T, pool *pgxpool.Pool, tenant, grant, nonce, idk string, attempt int) {
	t.Helper()
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO tenant (tenant_id, name) VALUES ('%s', 'T') ON CONFLICT DO NOTHING`, tenant),
		// The grant guard of migration 0012 (noema#168) refuses a grant whose decision record is absent or whose signed document does not name the record: the record comes
		// first and the grant's document names its decision and tenant and carries the bound fields, as the seed of schema_test.go does.
		fmt.Sprintf(`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		 VALUES ('dec_%s', '%s', '{"decision_id":"dec_%s","tenant_id":"%s","outcome":"ALLOW","sig":{"key_id":"k1","value":"aa"}}'::jsonb || `+testpg.BoundFieldsSQL+`, 'ALLOW', 'sha256:' || repeat('0', 64), '[]', 'k1', 'aa')`, grant, tenant, grant, tenant),
		fmt.Sprintf(`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value, status)
		 VALUES ('%s', '%s', 'dec_%s', '%s', '{"decision_id":"dec_%s","tenant_id":"%s"}'::jsonb || `+testpg.GrantBoundSQL+`, 'pol_1', '1', now() + interval '1 hour', 'k1', 'aa', 'ISSUED')`, grant, tenant, grant, nonce, grant, tenant),
		fmt.Sprintf(`INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ('%s', '%s', '%s', '%s', %d, now())`, grant, tenant, nonce, idk, attempt),
		fmt.Sprintf(`UPDATE decision_grant SET status = 'SPENT' WHERE grant_id = '%s'`, grant),
	} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
}

func newSigWorld(t *testing.T) *sigWorld {
	t.Helper()
	pool, dsn := migratedDSN(t)
	w := &sigWorld{pool: pool, dsn: dsn, keys: map[string]ed25519.PrivateKey{}}
	spend(t, pool, "ten_a", "grt_1", "n1", "idk_v1_a", 1)
	return w
}

// receipt is one receipt as the Controller would build it: the document without sig, and the row's columns.
type receipt struct {
	tenant, grant, id, idk, action, state, raw, ack string
	attempt                                         int
	dispatched                                      time.Time
	corr                                            string
}

func goodReceipt() receipt {
	return receipt{tenant: "ten_a", grant: "grt_1", id: "rcp_1", idk: "idk_v1_a", action: "fin.cap.case", state: "ACKNOWLEDGED", raw: "ACK_2XX:201", ack: "CASE-1",
		attempt: 1, dispatched: time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC), corr: "corr_1"}
}

func (r receipt) doc() map[string]any {
	var ack any
	if r.ack != "" {
		ack = r.ack
	}
	return map[string]any{"schema_version": "1.0", "correlation_id": r.corr, "receipt_id": r.id, "grant_id": r.grant, "tenant_id": r.tenant, "idempotency_key": r.idk,
		"logical_action": r.action, "attempt": int64(r.attempt), "dispatched_at": r.dispatched.Format(time.RFC3339Nano), "transport_result": strings.SplitN(r.raw, ":", 2)[0],
		"execution_state": r.state, "external_ack": ack}
}

// call is the receipt_insert argument list for r signed under keyID (the key must be in the world), or with sigOverride when set.
func (w *sigWorld) call(t *testing.T, r receipt, keyID, sigOverride string) []any {
	t.Helper()
	doc := r.doc()
	jcs, err := crypto.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	sig := sigOverride
	if sig == "" {
		priv, ok := w.keys[keyID]
		if !ok {
			t.Fatalf("no private key for %s", keyID)
		}
		sig = hex.EncodeToString(ed25519.Sign(priv, append([]byte(receiptTag), jcs...)))
	}
	withSig := map[string]any{}
	for k, v := range doc {
		withSig[k] = v
	}
	withSig["sig"] = map[string]any{"key_id": keyID, "value": sig}
	body, err := crypto.CanonicalJSON(withSig)
	if err != nil {
		t.Fatal(err)
	}
	var ack *string
	if r.ack != "" {
		a := r.ack
		ack = &a
	}
	return []any{r.id, r.tenant, r.grant, int64(r.attempt), r.idk, r.action, r.state, r.raw, ack, r.dispatched, string(body), keyID, sig, jcs, time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)}
}

const insertSQL = `SELECT receipt_insert($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,$15)`

func (w *sigWorld) receipts(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.pool.QueryRow(ctx(t), `SELECT count(*) FROM execution_receipt`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// insertAs runs receipt_insert as a role in a transaction it commits only on success.
func (w *sigWorld) insertAs(t *testing.T, role string, args []any) error {
	t.Helper()
	tx, err := w.pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := tx.Exec(ctx(t), `SET LOCAL ROLE `+role); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx(t), insertSQL, args...); err != nil {
		return err
	}
	return tx.Commit(ctx(t))
}

// A receipt signed by the registered core-execution key is stored through receipt_insert, by the role that holds EXECUTE on it.
func TestAGenuineReceiptIsStoredThroughReceiptInsert(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	if err := w.insertAs(t, "execution_rw", w.call(t, goodReceipt(), k1, "")); err != nil {
		t.Fatalf("a genuine receipt was refused: %v", err)
	}
	if w.receipts(t) != 1 {
		t.Fatalf("receipts = %d, want 1", w.receipts(t))
	}
	// The first writer wins: the same grant and attempt again stores nothing and says so.
	var inserted bool
	r2 := goodReceipt()
	r2.id = "rcp_2"
	if err := w.pool.QueryRow(ctx(t), insertSQL, w.call(t, r2, k1, "")...).Scan(&inserted); err != nil || inserted {
		t.Fatalf("a second receipt for the same grant and attempt: inserted=%v err=%v", inserted, err)
	}
}

// DEBT-01 closure check: a forged receipt (any 128 hex characters, a signature by another key, a missing signature) is refused by every path,
// and nothing is stored.
func TestAForgedReceiptIsRefusedByEveryPath(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	stranger := newKey(t)
	jcs, _ := crypto.CanonicalJSON(goodReceipt().doc())
	byStranger := hex.EncodeToString(ed25519.Sign(stranger, append([]byte(receiptTag), jcs...)))
	for name, sig := range map[string]string{"128 hex characters": strings.Repeat("ab", 64), "all zero": strings.Repeat("00", 64), "a stranger's key": byStranger,
		"64 hex characters (an HMAC's shape)": strings.Repeat("cd", 32), "upper case": strings.ToUpper(byStranger), "short": "abcd"} {
		err := w.insertAs(t, "execution_rw", w.call(t, goodReceipt(), k1, sig))
		wantCode(t, err, ns001, "receipt_insert with "+name)
	}
	// The same receipt signed correctly but with the signature member left out of the stored document or the row changed.
	good := w.call(t, goodReceipt(), k1, "")
	tamperedState := append([]any(nil), good...)
	tamperedState[6] = "REJECTED_STALE_VERSION" // the row says one thing, the signed bytes another
	wantCode(t, w.insertAs(t, "execution_rw", tamperedState), ns001, "row state differs from the signed bytes")
	// Direct INSERT: execution_rw has no INSERT privilege any more (D16).
	wantCode(t, try(t, w.pool, "execution_rw", `INSERT INTO execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, dispatched_at, json, sig_key_id, sig_value, signed_jcs, created_at)
		VALUES ('rcp_x', 'ten_a', 'grt_1', 1, 'idk_v1_a', 'A', 'OUTCOME_UNKNOWN', 'TIMEOUT', now(), '{}', 'k', 's', '\x7b7d', now())`), denied, "direct INSERT as execution_rw")
	// The owner and a superuser meet the trigger too: it is ENABLE ALWAYS, so session_replication_role = replica does not skip it.
	tx, err := w.pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := tx.Exec(ctx(t), `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	forged := w.call(t, goodReceipt(), k1, strings.Repeat("ab", 64))
	_, err = tx.Exec(ctx(t), `INSERT INTO execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, external_ack, dispatched_at, json, sig_key_id, sig_value, signed_jcs, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,$15)`, forged...)
	wantCode(t, err, ns001, "superuser INSERT under session_replication_role = replica")
	if w.receipts(t) != 0 {
		t.Fatalf("%d receipts stored after only refused inserts", w.receipts(t))
	}
}

// The stored document must be exactly the signed one plus the row's own signature member (definition of done 3c, A131: removing this check from the trigger went
// unseen). A genuinely signed receipt whose stored json carries an extra member, lacks a member, or names another signature is refused, and nothing is stored.
func TestAGenuineSignatureOverADifferentStoredDocumentIsRefused(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	genuine := w.call(t, goodReceipt(), k1, "")
	var doc map[string]any
	if err := json.Unmarshal([]byte(genuine[10].(string)), &doc); err != nil {
		t.Fatal(err)
	}
	withJSON := func(mutate func(map[string]any)) []any {
		d := map[string]any{}
		for k, v := range doc {
			d[k] = v
		}
		mutate(d)
		body, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		args := append([]any(nil), genuine...)
		args[10] = string(body)
		return args
	}
	for name, args := range map[string][]any{
		"an extra member":          withJSON(func(d map[string]any) { d["extra"] = "x" }),
		"another signature value":  withJSON(func(d map[string]any) { d["sig"] = map[string]any{"key_id": k1, "value": strings.Repeat("ab", 64)} }),
		"another signature key id": withJSON(func(d map[string]any) { d["sig"] = map[string]any{"key_id": "other", "value": genuine[12]} }),
	} {
		wantCode(t, w.insertAs(t, "execution_rw", args), ns001, "a genuine signature over "+name)
	}
	if w.receipts(t) != 0 {
		t.Fatalf("%d receipts stored after only refused inserts", w.receipts(t))
	}
}

// A genuine signature is bound to its tenant, grant, attempt, idempotency key, state and receipt: copying it onto any other row fails.
func TestAReplayedReceiptIsRefused(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	spend(t, w.pool, "ten_a", "grt_2", "n2", "idk_v1_b", 1)
	spend(t, w.pool, "ten_b", "grt_3", "n3", "idk_v1_a", 1)
	spend(t, w.pool, "ten_a", "grt_4", "n4", "idk_v1_a", 2)
	genuine := w.call(t, goodReceipt(), k1, "")
	sig := genuine[12].(string)
	cases := map[string]func(r *receipt){
		"another grant":           func(r *receipt) { r.grant = "grt_2" },
		"another tenant":          func(r *receipt) { r.tenant, r.grant = "ten_b", "grt_3" },
		"another attempt":         func(r *receipt) { r.grant, r.attempt = "grt_4", 2 },
		"another idempotency key": func(r *receipt) { r.idk = "idk_v1_b"; r.grant = "grt_2" },
		"another state": func(r *receipt) {
			r.state = "REJECTED_STALE_VERSION"
			r.raw = "VERSION_CONFLICT_409_412:409"
			r.ack = ""
		},
		"another receipt id":      func(r *receipt) { r.id = "rcp_9" },
		"another acknowledgement": func(r *receipt) { r.ack = "CASE-2" },
	}
	for name, mutate := range cases {
		r := goodReceipt()
		mutate(&r)
		// The attacker rebuilds the row and the document around the genuine signature: the bytes differ, so the signature cannot verify.
		args := w.call(t, r, k1, sig)
		wantCode(t, w.insertAs(t, "execution_rw", args), ns001, "replay onto "+name)
	}
	// Copying the genuine bytes unchanged onto a spend that does not match them fails the spend check (D7 check 2).
	wrongSpend := goodReceipt()
	wrongSpend.grant = "grt_2"
	args := w.call(t, goodReceipt(), k1, "")
	args[2] = wrongSpend.grant
	wantCode(t, w.insertAs(t, "execution_rw", args), ns001, "genuine bytes onto another grant's row")
	if w.receipts(t) != 0 {
		t.Fatalf("%d receipts stored after only refused replays", w.receipts(t))
	}
}

// Rotation (D5): two keys may be ACTIVE, the signer uses the newest, a third is refused, a retired key signs nothing new.
func TestRotationAcceptsTwoActiveKeysAndRefusesARetiredOne(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	k2 := w.register(t, "core-execution", 2)
	if _, err := w.pool.Exec(ctx(t), `SELECT key_register('core-execution', 3, $1)`, []byte(newKey(t).Public().(ed25519.PublicKey))); err == nil {
		t.Fatal("a third ACTIVE key was registered")
	}
	spend(t, w.pool, "ten_a", "grt_2", "n2", "idk_v1_b", 1)
	r1, r2 := goodReceipt(), goodReceipt()
	r2.grant, r2.idk, r2.id = "grt_2", "idk_v1_b", "rcp_2"
	if err := w.insertAs(t, "execution_rw", w.call(t, r1, k1, "")); err != nil {
		t.Fatalf("a receipt under the older ACTIVE key: %v", err)
	}
	if err := w.insertAs(t, "execution_rw", w.call(t, r2, k2, "")); err != nil {
		t.Fatalf("a receipt under the newer ACTIVE key: %v", err)
	}
	if _, err := w.pool.Exec(ctx(t), `SELECT key_retire($1)`, k1); err != nil {
		t.Fatal(err)
	}
	spend(t, w.pool, "ten_a", "grt_3", "n3", "idk_v1_c", 1)
	r3 := goodReceipt()
	r3.grant, r3.idk, r3.id = "grt_3", "idk_v1_c", "rcp_3"
	wantCode(t, w.insertAs(t, "execution_rw", w.call(t, r3, k1, "")), ns001, "a RETIRED key")
	if err := w.insertAs(t, "execution_rw", w.call(t, r3, k2, "")); err != nil {
		t.Fatalf("the remaining ACTIVE key: %v", err)
	}
	// With one slot free, a new version registers; an old version number does not.
	if _, err := w.pool.Exec(ctx(t), `SELECT key_register('core-execution', 2, $1)`, []byte(newKey(t).Public().(ed25519.PublicKey))); err == nil {
		t.Fatal("version 2 was registered twice")
	}
	if _, err := w.pool.Exec(ctx(t), `SELECT key_register('core-execution', 3, $1)`, []byte(newKey(t).Public().(ed25519.PublicKey))); err != nil {
		t.Fatalf("version 3 after a retirement: %v", err)
	}
	// Receipts signed while the key was active stay stored (retirement is routine).
	if w.receipts(t) != 3 {
		t.Fatalf("receipts = %d, want 3", w.receipts(t))
	}
}

// A revoked key signs nothing new, and the compromise time is kept for the chain verifier (D5).
func TestARevokedKeyIsRefusedAndRemembered(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	since := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if _, err := w.pool.Exec(ctx(t), `SELECT key_revoke($1, $2)`, k1, since); err != nil {
		t.Fatal(err)
	}
	wantCode(t, w.insertAs(t, "execution_rw", w.call(t, goodReceipt(), k1, "")), ns001, "a REVOKED key")
	var status string
	var got time.Time
	if err := w.pool.QueryRow(ctx(t), `SELECT status, compromised_since FROM signing_key WHERE key_id = $1`, k1).Scan(&status, &got); err != nil || status != "REVOKED" || !got.Equal(since) {
		t.Fatalf("status %q compromised_since %v err %v", status, got, err)
	}
	if _, err := w.pool.Exec(ctx(t), `SELECT key_revoke($1, $2)`, k1, since); err == nil {
		t.Fatal("a key was revoked twice")
	}
	// A key of another identity signs no receipt, even while ACTIVE (D2, D11).
	outcome := w.register(t, "core-outcome", 1)
	wantCode(t, w.insertAs(t, "execution_rw", w.call(t, goodReceipt(), outcome, "")), ns001, "an ACTIVE key of core-outcome")
	// A key id that names a key that was never registered.
	wantCode(t, w.insertAs(t, "execution_rw", w.call(t, goodReceipt(), "core-execution.ed25519.9", strings.Repeat("ab", 64))), ns001, "an unknown key id")
}

// D13: when verification is unavailable the insert fails, no row is added, and nothing else of its transaction is kept.
func TestWhenVerificationIsUnavailableTheInsertFailsClosed(t *testing.T) {
	for name, break_ := range map[string]string{
		"the extension is dropped": `DROP EXTENSION pgsodium CASCADE`,
		"the verification function raises": `ALTER FUNCTION pgsodium.crypto_sign_verify_detached(bytea, bytea, bytea) RENAME TO crypto_sign_verify_detached_real;
			CREATE FUNCTION pgsodium.crypto_sign_verify_detached(bytea, bytea, bytea) RETURNS boolean LANGUAGE plpgsql AS $f$ BEGIN RAISE EXCEPTION 'verification unavailable'; END $f$;
			GRANT EXECUTE ON FUNCTION pgsodium.crypto_sign_verify_detached(bytea, bytea, bytea) TO noema_migrator`,
	} {
		t.Run(name, func(t *testing.T) {
			w := newSigWorld(t)
			k1 := w.register(t, "core-execution", 1)
			args := w.call(t, goodReceipt(), k1, "")
			if _, err := w.pool.Exec(ctx(t), break_); err != nil {
				t.Fatal(err)
			}
			tx, err := w.pool.Begin(ctx(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_marker', 'M')`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx(t), insertSQL, args...); err == nil {
				t.Fatal("a correctly signed receipt was stored while verification was unavailable")
			}
			_ = tx.Rollback(ctx(t))
			var markers int
			if err := w.pool.QueryRow(ctx(t), `SELECT count(*) FROM tenant WHERE tenant_id = 'ten_marker'`).Scan(&markers); err != nil || markers != 0 {
				t.Fatalf("another write of the failed transaction was kept (%d), %v", markers, err)
			}
			if w.receipts(t) != 0 {
				t.Fatalf("%d receipts stored", w.receipts(t))
			}
		})
	}
}

// D14, D16: no runtime role can disable, replace or bypass the control, or change the key table.
func TestNoRuntimeRoleCanTouchTheControl(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	for _, role := range []string{"execution_rw", "trust_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "record_append"} {
		for _, stmt := range []string{
			`ALTER TABLE execution_receipt DISABLE TRIGGER execution_receipt_verify`,
			`DROP TRIGGER execution_receipt_verify ON execution_receipt`,
			`ALTER TABLE execution_receipt DISABLE TRIGGER ALL`,
			`CREATE OR REPLACE FUNCTION noema_sig.verify(text, text, text, bytea, text) RETURNS boolean LANGUAGE sql AS 'SELECT true'`,
			`CREATE OR REPLACE FUNCTION execution_receipt_verify() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NEW; END'`,
			`ALTER FUNCTION noema_sig.verify(text, text, text, bytea, text) SET search_path = public`,
			`DROP FUNCTION noema_sig.verify(text, text, text, bytea, text)`,
			`INSERT INTO signing_key (key_id, identity, version, public_key, status, registered_at) VALUES ('core-execution.ed25519.7', 'core-execution', 7, '\x00', 'ACTIVE', now())`,
			`UPDATE signing_key SET status = 'ACTIVE'`,
			`DELETE FROM signing_key`,
			`TRUNCATE signing_key`,
			`SELECT key_register('core-execution', 8, '\x00')`,
			`SELECT key_retire('core-execution.ed25519.1')`,
			`SELECT key_revoke('core-execution.ed25519.1', now())`,
			`SELECT noema_sig.verify('receipt', 'core-execution', 'core-execution.ed25519.1', '\x00', 'aa')`,
			`SELECT pgsodium.crypto_sign_verify_detached('\x00', '\x00', '\x00')`,
			`SET session_replication_role = replica`,
			`DELETE FROM execution_receipt`,
			`UPDATE execution_receipt SET signed_jcs = '\x7b7d'`,
			`TRUNCATE execution_receipt`,
			`CREATE TABLE sneaky (i int)`,
			`CREATE SCHEMA sneaky`,
			`DROP EXTENSION pgsodium CASCADE`,
		} {
			wantCode(t, try(t, w.pool, role, stmt), denied, role+": "+stmt)
		}
	}
	// What they may do: read the public keys (D16).
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "record_append"} {
		if err := try(t, w.pool, role, `SELECT key_id, public_key FROM signing_key`); err != nil {
			t.Errorf("%s cannot read the public keys: %v", role, err)
		}
	}
	// None is a member of noema_migrator, directly or through another role, and none has a privileged attribute (D14).
	rows, err := w.pool.Query(ctx(t), `WITH RECURSIVE m(roleid, member) AS (
		SELECT roleid, member FROM pg_auth_members UNION SELECT a.roleid, m.member FROM pg_auth_members a JOIN m ON a.member = m.roleid)
		SELECT r.rolname FROM m JOIN pg_roles r ON r.oid = m.member JOIN pg_roles g ON g.oid = m.roleid
		 WHERE g.rolname = 'noema_migrator' AND r.rolname IN ('execution_rw','trust_rw','outcome_rw','evidence_rw','orchestrator_rw','record_append')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		t.Errorf("%s is a member of noema_migrator", n)
	}
	var bad int
	if err := w.pool.QueryRow(ctx(t), `SELECT count(*) FROM pg_roles WHERE rolname IN ('execution_rw','trust_rw','outcome_rw','evidence_rw','orchestrator_rw','record_append')
		AND (rolsuper OR rolcreaterole OR rolcreatedb OR rolreplication OR rolbypassrls)`).Scan(&bad); err != nil || bad != 0 {
		t.Fatalf("%d runtime roles hold a privileged attribute (%v)", bad, err)
	}
	_ = k1
}

// K1 and D5 as the table itself enforces them, even for the owner and a superuser who skip the three functions.
func TestTheKeyTableKeepsItsInvariantsForTheOwnerToo(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	pub := []byte(newKey(t).Public().(ed25519.PublicKey))
	if _, err := w.pool.Exec(ctx(t), `SELECT key_register('model-service', 1, $1)`, pub); err == nil {
		t.Error("an identity without a signing key (model-service, OD-P-73) was registered")
	}
	if _, err := w.pool.Exec(ctx(t), `SELECT key_register('core-execution', 2, $1)`, pub[:31]); err == nil {
		t.Error("a 31-byte public key was registered")
	}
	if _, err := w.pool.Exec(ctx(t), `SELECT key_register('Core-Execution', 2, $1)`, pub); err == nil {
		t.Error("a malformed identity was registered")
	}
	for _, q := range []string{
		// Without the three functions: no change at all, because the guard wants their setting.
		`INSERT INTO signing_key (key_id, identity, version, public_key, status, registered_at) VALUES ('core-execution.ed25519.2', 'core-execution', 2, '\x` + strings.Repeat("11", 32) + `', 'ACTIVE', now())`,
		`UPDATE signing_key SET status = 'REVOKED', compromised_since = now()`,
		`DELETE FROM signing_key`,
		`TRUNCATE signing_key`,
	} {
		if _, err := w.pool.Exec(ctx(t), q); err == nil {
			t.Errorf("the owner changed the key table without the key functions: %s", q)
		}
	}
	// A key id that does not match its identity and version cannot exist (K1), even through the functions' setting.
	tx, err := w.pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := tx.Exec(ctx(t), `SELECT set_config('noema_sig.key_change', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx(t), `INSERT INTO signing_key (key_id, identity, version, public_key, status, registered_at) VALUES ('core-execution.ed25519.9', 'core-execution', 2, $1, 'ACTIVE', now())`, pub); err == nil {
		t.Error("a key id that names another version was stored (K1)")
	}
	_ = k1
}

// The DEBT-01 closure test, on the held noema#94 guard: with the guard of migration 0002 (testdata) installed, a forged
// REJECTED_STALE_VERSION receipt cannot be stored by execution_rw by INSERT, receipt_insert or any function it may execute, and the
// SPENT grant cannot become VOID. The negative control shows the test sees the bypass: with the receipt trigger switched off by the owner
// (what the held code faced), the same forgery voids the grant.
func TestAForgedReceiptCannotVoidASpentGrantOnTheHeldGuard(t *testing.T) {
	// The held guard accepts any 64 lower-case hex characters as a signature (the shape of an HMAC; the database holds no key). That is
	// DEBT-01: the forger writes exactly that.
	forge := func(w *sigWorld, sig string) []any {
		r := goodReceipt()
		r.state, r.raw, r.ack = "REJECTED_STALE_VERSION", "VERSION_CONFLICT_409_412:409", ""
		return w.call(t, r, "core-execution.ed25519.1", sig)
	}
	forged := strings.Repeat("ab", 32)
	apply := func(t *testing.T, w *sigWorld) {
		sqlText, err := os.ReadFile("../../migrations/testdata/0002_grant_spent_void.noema94.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.pool.Exec(ctx(t), string(sqlText)); err != nil {
			t.Fatalf("apply the held guard: %v", err)
		}
	}
	t.Run("with the fix", func(t *testing.T) {
		w := newSigWorld(t)
		w.register(t, "core-execution", 1)
		apply(t, w)
		wantCode(t, w.insertAs(t, "execution_rw", forge(w, forged)), ns001, "forged receipt through receipt_insert")
		wantCode(t, w.insertAs(t, "execution_rw", forge(w, strings.Repeat("ab", 64))), ns001, "forged 128-hex receipt through receipt_insert")
		// Through the tenant's own execution login: since noema#168 row security hides a grant from an unmapped session, so SET ROLE from the administrator would update no row and see no guard.
		own := testpg.TenantLogin(t, w.pool, w.dsn, "execution_rw", "ten_a")
		voidTx, err := own.Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		_, voidErr := voidTx.Exec(ctx(t), `UPDATE decision_grant SET status = 'VOID' WHERE grant_id = 'grt_1'`)
		_ = voidTx.Rollback(ctx(t))
		wantCode(t, voidErr, violation, "void without a receipt")
		var status string
		if err := w.pool.QueryRow(ctx(t), `SELECT status FROM decision_grant WHERE grant_id = 'grt_1'`).Scan(&status); err != nil || status != "SPENT" {
			t.Fatalf("grant status %q, %v", status, err)
		}
	})
	t.Run("negative control: the trigger off and INSERT granted (the held code's world)", func(t *testing.T) {
		w := newSigWorld(t)
		w.register(t, "core-execution", 1)
		apply(t, w)
		for _, q := range []string{`ALTER TABLE execution_receipt DISABLE TRIGGER execution_receipt_verify`, `GRANT INSERT ON execution_receipt TO execution_rw`} {
			if _, err := w.pool.Exec(ctx(t), q); err != nil {
				t.Fatal(err)
			}
		}
		tx, err := w.pool.Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx(t)) }()
		if _, err := tx.Exec(ctx(t), `SET LOCAL ROLE execution_rw`); err != nil {
			t.Fatal(err)
		}
		a := forge(w, forged)
		if _, err := tx.Exec(ctx(t), `INSERT INTO execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, external_ack, dispatched_at, json, sig_key_id, sig_value, signed_jcs, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,$15)`, a...); err != nil {
			t.Fatalf("the control's forgery was refused: %v", err)
		}
		if _, err := tx.Exec(ctx(t), `UPDATE decision_grant SET status = 'VOID' WHERE grant_id = 'grt_1'`); err != nil {
			t.Fatalf("the control's void was refused (the test would not see the bypass): %v", err)
		}
	})
}

// D7 check 2 on its own: a receipt signed by the genuine key and consistent with itself, whose tenant, attempt or idempotency key still
// differs from what E8 recorded in grant_spend, is refused. (The replay test above fails earlier, on the document-to-row check.)
func TestAGenuinelySignedReceiptThatContradictsTheSpendIsRefused(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	spend(t, w.pool, "ten_a", "grt_2", "n2", "idk_v1_b", 1)
	if _, err := w.pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(r *receipt){
		"a different attempt":         func(r *receipt) { r.grant, r.idk, r.attempt = "grt_2", "idk_v1_b", 2 },
		"a different idempotency key": func(r *receipt) { r.grant, r.idk = "grt_2", "idk_v1_other" },
		"a different tenant":          func(r *receipt) { r.grant, r.idk, r.tenant = "grt_2", "idk_v1_b", "ten_b" },
	} {
		r := goodReceipt()
		mutate(&r)
		wantCode(t, w.insertAs(t, "execution_rw", w.call(t, r, k1, "")), ns001, name+" than the spend recorded")
	}
	if w.receipts(t) != 0 {
		t.Fatalf("%d receipts stored", w.receipts(t))
	}
}

// D11 on its own: the verifier answers false for a valid signature whose key belongs to another identity than the purpose allows, and for a
// key id that is not its row. (The trigger always passes core-execution, so only a direct call reaches this; no runtime role can make one.)
func TestTheVerifierBindsPurposeToIdentity(t *testing.T) {
	w := newSigWorld(t)
	w.register(t, "core-execution", 1)
	outcome := w.register(t, "core-outcome", 1)
	msg := []byte(receiptTag + `{"x":1}`)
	sig := hex.EncodeToString(ed25519.Sign(w.keys[outcome], msg))
	check := func(purpose, claimed, keyID string) bool {
		var ok bool
		if err := w.pool.QueryRow(ctx(t), `SELECT noema_sig.verify($1, $2, $3, $4, $5)`, purpose, claimed, keyID, msg, sig).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !check("outcome-verdict", "core-outcome", outcome) {
		t.Fatal("a valid verdict signature by core-outcome's key was refused")
	}
	if check("receipt", "core-outcome", outcome) {
		t.Error("core-outcome's key verified a receipt")
	}
	if check("receipt", "core-execution", outcome) {
		t.Error("a key verified under another identity's name")
	}
	if check("outcome-verdict", "core-execution", outcome) {
		t.Error("a verdict verified under the wrong identity")
	}
	if check("unknown-purpose", "core-outcome", outcome) {
		t.Error("an unknown purpose verified")
	}
}

// D10a "guard against re-enabling": an extension upgrade or a re-install can re-create or re-enable pgsodium's event trigger, which breaks every later
// DDL command. db.Migrate checks first and refuses to run if any event trigger owned by the extension is not disabled.
func TestMigrateRefusesWhileAnExtensionEventTriggerIsEnabled(t *testing.T) {
	pool, _ := newBareDatabase(t)
	var name string
	if err := pool.QueryRow(ctx(t), `SELECT e.evtname FROM pg_event_trigger e JOIN pg_depend d ON d.objid = e.oid AND d.deptype = 'e'
		JOIN pg_extension x ON x.oid = d.refobjid AND x.extname = 'pgsodium' LIMIT 1`).Scan(&name); err != nil {
		t.Fatalf("pgsodium installs no event trigger in this cluster: %v", err)
	}
	if _, err := pool.Exec(ctx(t), `ALTER EVENT TRIGGER `+name+` ENABLE ALWAYS`); err != nil {
		t.Fatal(err)
	}
	if applied, err := db.Migrate(ctx(t), pool, migrations.FS); err == nil || !strings.Contains(err.Error(), "event trigger") || len(applied) != 0 {
		t.Fatalf("Migrate with %s enabled: applied %v, err %v; want a refusal that names the event trigger and applies nothing", name, applied, err)
	}
	var tables int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM information_schema.tables WHERE table_name IN ('schema_migration', 'execution_receipt')`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("a refused Migrate left %d tables (%v)", tables, err)
	}
	if _, err := pool.Exec(ctx(t), `ALTER EVENT TRIGGER `+name+` DISABLE`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx(t), pool, migrations.FS); err != nil {
		t.Fatalf("Migrate with every extension event trigger disabled: %v", err)
	}
}

// newBareDatabase is a database that testpg has bootstrapped but that has no migration yet.
func newBareDatabase(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	return testpg.NewDatabase(t)
}

// Codex review of noema#229 (P2), REQ-AUD-006, DEC-2026-11-01 D5: two registrations for one identity that overlap in time must not leave three ACTIVE
// keys. Transaction A registers version 2 and has not committed; B registers version 3. B must wait for A (a per-identity lock), then see two ACTIVE
// keys and be refused. Without the lock B passes at once, both commit, and the identity has three ACTIVE keys.
func TestConcurrentKeyRegistrationNeverLeavesThreeActiveKeys(t *testing.T) {
	w := newSigWorld(t)
	w.register(t, "core-execution", 1)
	a, err := w.pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Rollback(ctx(t)) }()
	if _, err := a.Exec(ctx(t), `SELECT key_register('core-execution', 2, $1)`, []byte(newKey(t).Public().(ed25519.PublicKey))); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		b, err := w.pool.Begin(ctx(t))
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = b.Rollback(ctx(t)) }()
		if _, err := b.Exec(ctx(t), `SELECT key_register('core-execution', 3, $1)`, []byte(newKey(t).Public().(ed25519.PublicKey))); err != nil {
			done <- err
			return
		}
		done <- b.Commit(ctx(t))
	}()
	var bErr error
	select {
	case bErr = <-done: // B did not wait for A: the registration was not serialised
	case <-time.After(2 * time.Second):
		if err := a.Commit(ctx(t)); err != nil {
			t.Fatal(err)
		}
		bErr = <-done
	}
	if bErr == nil {
		t.Errorf("the second registration succeeded while the first was open: three ACTIVE keys (rotation overlap exceeded)")
	}
	var active int
	if err := w.pool.QueryRow(ctx(t), `SELECT count(*) FROM signing_key WHERE identity = 'core-execution' AND status = 'ACTIVE'`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active > 2 {
		t.Fatalf("%d ACTIVE keys for core-execution, want at most 2", active)
	}
}
