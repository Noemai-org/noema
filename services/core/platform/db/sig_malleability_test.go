package db_test

// DEC-2026-11-01 D6, D13, row 4 of the test table; SR's findings 1 and 2 on noema#229. REQ-AUD-006, INV-04. The migration, not only the Go verifier and the
// spike, refuses the malleable and degenerate Ed25519 forms, and a malformed signed field is a signature failure (NS001, which the Go code maps to a
// SECURITY_EVENT), not a stray cast error. All keys and receipts are SYNTHETIC.

import (
	"crypto/ed25519"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// callDoc is call with a mutation of the signed document before it is signed: the row stays the genuine receipt's, the signed bytes differ.
func (w *sigWorld) callDoc(t *testing.T, r receipt, keyID string, mutate func(map[string]any)) []any {
	t.Helper()
	doc := r.doc()
	mutate(doc)
	jcs, err := crypto.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	sig := hex.EncodeToString(ed25519.Sign(w.keys[keyID], append([]byte(receiptTag), jcs...)))
	withSig := map[string]any{"sig": map[string]any{"key_id": keyID, "value": sig}}
	for k, v := range doc {
		withSig[k] = v
	}
	body, err := crypto.CanonicalJSON(withSig)
	if err != nil {
		t.Fatal(err)
	}
	args := w.call(t, r, keyID, "")
	args[10], args[12], args[13] = string(body), sig, jcs
	return args
}

// SR finding 2: a signed timestamp that is not a timestamp raised 22007 or 22008 from the cast, not NS001.
func TestAMalformedSignedTimestampIsASignatureFailure(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	for name, bad := range map[string]any{"text": "not-a-time", "an empty string": "", "month 13": "2026-13-45T09:00:00Z", "a number": 1730000000, "an hour of 25": "2026-11-06T25:00:00Z"} {
		args := w.callDoc(t, goodReceipt(), k1, func(d map[string]any) { d["dispatched_at"] = bad })
		wantCode(t, w.insertAs(t, "execution_rw", args), ns001, "a signed dispatched_at of "+name)
	}
	if w.receipts(t) != 0 {
		t.Fatalf("%d receipts stored after only refused inserts", w.receipts(t))
	}
}

// SR finding 1: the five malleability and degenerate-key vectors of the spike, against the migration.
func TestMalleableAndDegenerateEd25519FormsAreRefusedByTheMigration(t *testing.T) {
	w := newSigWorld(t)
	k1 := w.register(t, "core-execution", 1)
	good := w.call(t, goodReceipt(), k1, "")
	sig, _ := hex.DecodeString(good[12].(string))

	// S plus the group order L: the same point, a different encoding.
	l, _ := new(big.Int).SetString("7237005577332262213973186563042994240857116359379907606001950938285454250989", 10)
	s := new(big.Int).SetBytes(reverseBytes(sig[32:]))
	s.Add(s, l)
	sb := s.Bytes()
	if len(sb) > 32 {
		t.Fatalf("S plus L does not fit in 32 bytes (%d): the vector cannot be built", len(sb))
	}
	mal := append(append([]byte{}, sig[:32]...), reverseBytes(append(make([]byte, 32-len(sb)), sb...))...)
	args := append([]any(nil), good...)
	args[12] = hex.EncodeToString(mal)
	wantCode(t, w.insertAs(t, "execution_rw", args), ns001, "S plus L")

	// All-zero signature.
	args = append([]any(nil), good...)
	args[12] = strings.Repeat("00", 64)
	wantCode(t, w.insertAs(t, "execution_rw", args), ns001, "an all-zero signature")

	// A small-order public key (the identity point) with the identity point as R and S = 0, and a non-canonical encoding of the identity (y = p + 1):
	// both are registered the way deploy would register any 32 bytes (key_register checks length only), and the verifier must still refuse them.
	identity := append([]byte{1}, make([]byte, 31)...)
	nonCanonical := append(append([]byte{0xee}, []byte(strings.Repeat("\xff", 30))...), 0x7f)
	for name, pub := range map[string][]byte{"a small-order public key": identity, "a non-canonical public key": nonCanonical} {
		w2 := newSigWorld(t) // a core-execution identity may hold two ACTIVE keys: a genuine one to build the row with, and the degenerate one
		g := w2.register(t, "core-execution", 1)
		var id string
		if err := w2.pool.QueryRow(ctx(t), `SELECT key_register($1, $2, $3)`, "core-execution", 2, pub).Scan(&id); err != nil {
			t.Fatalf("key_register for %s: %v", name, err)
		}
		forged := w2.call(t, goodReceipt(), g, "") // the row and the signed document of a genuine receipt, claimed under the degenerate key
		forged[11] = id
		forged[12] = hex.EncodeToString(append(append([]byte{}, identity...), make([]byte, 32)...)) // R = identity point, S = 0
		wantCode(t, w2.insertAs(t, "execution_rw", forged), ns001, "a signature under "+name)
		if w2.receipts(t) != 0 {
			t.Fatalf("%s: %d receipts stored after only refused inserts", name, w2.receipts(t))
		}
	}
}

func reverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[len(b)-1-i] = b[i]
	}
	return out
}
