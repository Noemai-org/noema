// Package sigfixture makes SYNTHETIC Ed25519 keys and signed receipts for tests that must not import Execution's signer (Outcome's tests, for
// one: the verifier is independent of Execution, SoT 13.1). It signs the way DEC-2026-11-01 D6 says (a domain tag, then the RFC 8785 bytes of the
// document without sig), with the standard library and platform/crypto only, and stores through receipt_insert, the only path to execution_receipt.
// Nothing here is used outside tests, and no key it makes is a secret (AGENT-25).
package sigfixture

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// Identity is the one identity that signs receipts (D2).
const Identity = "core-execution"

const receiptTag = "noema/receipt/v1\n"

// createdAt is the SYNTHETIC instant the fixture stamps on the row (the clock rule: no time.Now outside platform/clock).
var createdAt = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

// Key is a SYNTHETIC core-execution key. Its seed is derived from the label and the version, so a run is repeatable.
type Key struct {
	ID      string
	Version int
	Priv    ed25519.PrivateKey
}

// NewKey makes version v of the key for label.
func NewKey(label string, v int) Key {
	seed := sha256.Sum256([]byte(fmt.Sprintf("SYNTHETIC sigfixture %s %d", label, v)))
	return Key{ID: fmt.Sprintf("%s.ed25519.%d", Identity, v), Version: v, Priv: ed25519.NewKeyFromSeed(seed[:])}
}

func timeout(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// Register adds the public halves of the keys to signing_key through key_register, oldest version first, with a pool that may call it.
func Register(t testing.TB, admin *pgxpool.Pool, keys ...Key) {
	t.Helper()
	for _, k := range keys {
		if _, err := admin.Exec(timeout(t), `SELECT key_register($1, $2, $3)`, Identity, k.Version, []byte(k.Priv.Public().(ed25519.PublicKey))); err != nil {
			t.Fatalf("sigfixture: key_register %s: %v", k.ID, err)
		}
	}
}

// Receipt is a SYNTHETIC receipt as a test plants it: the fields the signed document binds (D7).
type Receipt struct {
	ID, Tenant, Grant, IdempotencyKey, LogicalAction, State, Raw, Ack, Correlation string
	Attempt                                                                        int
	Dispatched                                                                     time.Time
}

// InsertReceipt signs r with key and stores it through receipt_insert; the database verifies the signature like any other (there is no back door).
func InsertReceipt(t testing.TB, pool *pgxpool.Pool, key Key, r Receipt) {
	t.Helper()
	var ack any
	if r.Ack != "" {
		ack = r.Ack
	}
	corr := r.Correlation
	if corr == "" {
		corr = "corr_abc123"
	}
	class, _, _ := strings.Cut(r.Raw, ":")
	doc := map[string]any{"schema_version": "1.0", "correlation_id": corr, "receipt_id": r.ID, "grant_id": r.Grant, "tenant_id": r.Tenant,
		"idempotency_key": r.IdempotencyKey, "logical_action": r.LogicalAction, "attempt": int64(r.Attempt), "dispatched_at": r.Dispatched.UTC().Format(time.RFC3339Nano),
		"transport_result": class, "execution_state": r.State, "external_ack": ack}
	signed, err := crypto.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	sig := hex.EncodeToString(ed25519.Sign(key.Priv, append([]byte(receiptTag), signed...)))
	withSig := map[string]any{}
	for k, v := range doc {
		withSig[k] = v
	}
	withSig["sig"] = map[string]any{"key_id": key.ID, "value": sig}
	body, err := crypto.CanonicalJSON(withSig)
	if err != nil {
		t.Fatal(err)
	}
	var ackCol *string
	if r.Ack != "" {
		a := r.Ack
		ackCol = &a
	}
	var inserted bool
	if err := pool.QueryRow(timeout(t), `SELECT receipt_insert($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15)`,
		r.ID, r.Tenant, r.Grant, int64(r.Attempt), r.IdempotencyKey, r.LogicalAction, r.State, r.Raw, ackCol, r.Dispatched, string(body), key.ID, sig, signed, createdAt).Scan(&inserted); err != nil || !inserted {
		t.Fatalf("sigfixture: receipt_insert: inserted=%v err=%v", inserted, err)
	}
}
