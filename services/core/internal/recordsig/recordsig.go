// Package recordsig makes SYNTHETIC Ed25519 signers for the Record appender in tests (REC-08, DEC-2026-11-01 D2, D6): one key per signing identity, registered in
// the database through key_register as the deploy step does, the private half held only here. A world that already has a signer for an identity (the receipt
// ring of Execution, for core-execution) passes it in as base and keeps it. Nothing here is used outside tests, and no key it makes is a secret (AGENT-25).
package recordsig

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// Identities are the identities that hold a signing key (migration 0013, D2).
var Identities = []string{"core-execution", "core-outcome", "core-trust", "core-evidence", "core-workflows", "core-edge", "core-record", "admin"}

const tag = "noema/record-event/v1\n"

type signer struct {
	keyID string
	priv  ed25519.PrivateKey
}

// SignRecordEvent signs the RFC 8785 bytes of the body under the record-event domain tag (D6).
func (s signer) SignRecordEvent(body map[string]any) (string, string, []byte, error) {
	jcs, err := crypto.CanonicalJSON(body)
	if err != nil {
		return "", "", nil, err
	}
	return s.keyID, hex.EncodeToString(ed25519.Sign(s.priv, append([]byte(tag), jcs...))), jcs, nil
}

// All registers version 1 of a key for every signing identity that base has no signer for, with the pool that may call key_register (the migration role),
// and returns base plus the new signers. The seeds come from label and the identity, so a run is repeatable.
func All(t testing.TB, admin *pgxpool.Pool, label string, base map[string]chain.Signer) map[string]chain.Signer {
	t.Helper()
	return For(t, admin, label, base, Identities...)
}

// For is All for the listed identities only: a world that registered a key for another identity by other means (Outcome's receipt fixture holds a
// core-execution key) asks for the identities it writes as.
func For(t testing.TB, admin *pgxpool.Pool, label string, base map[string]chain.Signer, identities ...string) map[string]chain.Signer {
	t.Helper()
	out := map[string]chain.Signer{}
	for k, v := range base {
		out[k] = v
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, id := range identities {
		if _, ok := out[id]; ok {
			continue
		}
		priv := synthKey(label, id)
		pub := []byte(priv.Public().(ed25519.PublicKey))
		keyID := id + ".ed25519.1"
		var have []byte
		switch err := admin.QueryRow(ctx, `SELECT public_key FROM signing_key WHERE key_id = $1`, keyID).Scan(&have); {
		case err == nil:
			if !bytes.Equal(have, pub) { // the identity already has a different version 1: another signer owns it
				t.Fatalf("recordsig: %s is already registered with another key", keyID)
			}
		case errors.Is(err, pgx.ErrNoRows):
			if err := admin.QueryRow(ctx, `SELECT key_register($1, 1, $2)`, id, pub).Scan(&keyID); err != nil {
				t.Fatalf("recordsig: key_register %s: %v", id, err)
			}
		default:
			t.Fatalf("recordsig: read signing_key: %v", err)
		}
		out[id] = signer{keyID: keyID, priv: priv}
	}
	return out
}

// synthKey is the SYNTHETIC private key of an identity in a world named label. The seed is fixed by label and identity, so a second process of the same test (the crash
// child of TST-05, which cannot reach the administrator pool) derives the same key the parent registered.
func synthKey(label, id string) ed25519.PrivateKey {
	sum := sha256.Sum256([]byte(fmt.Sprintf("SYNTHETIC recordsig %s %s", label, id)))
	return ed25519.NewKeyFromSeed(sum[:])
}

// Local returns the signers of For without touching the database: for a process that is started by a test whose parent registered the keys (All or For with the same
// label). It registers nothing, so a key the parent did not register is refused by the database, as it should be.
func Local(label string, base map[string]chain.Signer, identities ...string) map[string]chain.Signer {
	out := map[string]chain.Signer{}
	for k, v := range base {
		out[k] = v
	}
	for _, id := range identities {
		if _, ok := out[id]; ok {
			continue
		}
		out[id] = signer{keyID: id + ".ed25519.1", priv: synthKey(label, id)}
	}
	return out
}
