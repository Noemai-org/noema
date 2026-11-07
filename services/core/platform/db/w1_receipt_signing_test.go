package db_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// SEC-13, REQ-AUD-006, REQ-EXE-004, DEBT-01 closure criteria (2) to (5). Written against the signature format of the Proposed record
// DEC-2026-11-01-receipt-and-record-signing (docs#80) D4 and D6 as of 3 Nov 2026; adjust when W3 (EXE-07) lands. SYNTHETIC test keys.
//
// W1 format: key_id "<identity>.ed25519.<version>"; sig.value 128 lower-case hex; the signed message is the domain tag
// "noema/receipt/v1\n" followed by the RFC 8785 bytes of the receipt document without sig.

const (
	w1TagReceipt = "noema/receipt/v1\n"
	w1TagEvent   = "noema/record-event/v1\n"
)

var w1KeyID = regexp.MustCompile(`^[a-z][a-z-]*\.ed25519\.[1-9][0-9]*$`)
var w1SigHex = regexp.MustCompile(`^[0-9a-f]{128}$`)

// w1Key is a synthetic Ed25519 key for one identity and version. The seed is derived from a public label, so it is not a secret.
type w1Key struct {
	identity string
	version  int
	priv     ed25519.PrivateKey
}

func newW1Key(identity string, version int) w1Key {
	seed := sha256.Sum256([]byte(fmt.Sprintf("SYNTHETIC SEC-13 %s v%d", identity, version)))
	return w1Key{identity, version, ed25519.NewKeyFromSeed(seed[:])}
}

func (k w1Key) keyID() string  { return fmt.Sprintf("%s.ed25519.%d", k.identity, k.version) }
func (k w1Key) public() []byte { return k.priv.Public().(ed25519.PublicKey) }

// signW1 returns the exact bytes that were signed (without the tag), the key id and the 128-hex signature, for a document with no sig member.
func signW1(k w1Key, tag string, doc map[string]any) (signedJCS []byte, keyID, sigHex string, err error) {
	jcs, err := crypto.CanonicalJSON(doc)
	if err != nil {
		return nil, "", "", err
	}
	sig := ed25519.Sign(k.priv, append([]byte(tag), jcs...))
	return jcs, k.keyID(), hex.EncodeToString(sig), nil
}

// The signature format is pinned against an independent implementation. The constants below were computed with a separate
// standard-library RFC 8032 implementation in Python (deploy/spike/pgsodium/gen_vectors.py, which asserts RFC 8032 TEST 1 and 2 before
// it writes anything) over a hand-written JCS of the same document, not with this package. A disagreement means the Go signer or the
// canonical JSON differs from the format W1 specifies.
func TestW1ReceiptSignatureMatchesAnIndependentImplementation(t *testing.T) {
	const (
		wantPublic = "31477fc5ca3c726055025895f136cfb62a27682884b4ebfb4475d4a74288fb61"
		wantJCS    = `{"attempt":1,"correlation_id":"corr_1","dispatched_at":"2026-11-03T09:00:00Z","execution_state":"REJECTED_STALE_VERSION","external_ack":null,"grant_id":"grt_1","idempotency_key":"idk_v1_grt_1","logical_action":"ROUTE_CASE","receipt_id":"rcp_grt_1_1","schema_version":"1.0","tenant_id":"ten_a","transport_result":"VERSION_CONFLICT_409_412"}`
		wantSig    = "61e6124921fe4284207a6edea9c792749bb4c96de9e613b8f8e25680ab6d3c2dc1fc98c47d0b691850bab15d82f9bd74b6fc3bec2539e2aa8a1b24f2aa507609"
	)
	k := newW1Key("core-execution", 1)
	doc := map[string]any{
		"schema_version": "1.0", "tenant_id": "ten_a", "correlation_id": "corr_1", "receipt_id": "rcp_grt_1_1", "grant_id": "grt_1",
		"idempotency_key": "idk_v1_grt_1", "logical_action": "ROUTE_CASE", "attempt": 1, "dispatched_at": "2026-11-03T09:00:00Z",
		"transport_result": "VERSION_CONFLICT_409_412", "execution_state": "REJECTED_STALE_VERSION", "external_ack": nil,
	}
	jcs, keyID, sigHex, err := signW1(k, w1TagReceipt, doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(k.public()); got != wantPublic {
		t.Fatalf("public key %s, independent implementation %s", got, wantPublic)
	}
	if string(jcs) != wantJCS {
		t.Fatalf("canonical bytes differ from the hand-written JCS:\n got %s\nwant %s", jcs, wantJCS)
	}
	if sigHex != wantSig {
		t.Fatalf("signature %s, independent implementation %s", sigHex, wantSig)
	}
	if !w1KeyID.MatchString(keyID) || !w1SigHex.MatchString(sigHex) {
		t.Fatalf("key id %q or signature %q is outside the W1 format (D4)", keyID, sigHex)
	}
}

// Verification is exact: the tag, every byte of the document, the key and the signature all matter (W1 D6; the cases the database
// must also refuse, in w1_receipt_matrix_test.go).
func TestW1ReceiptSignatureBindsEverythingItShould(t *testing.T) {
	k := newW1Key("core-execution", 1)
	doc := map[string]any{"tenant_id": "ten_a", "grant_id": "grt_1", "attempt": 1, "execution_state": "REJECTED_STALE_VERSION"}
	jcs, _, sigHex, err := signW1(k, w1TagReceipt, doc)
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := hex.DecodeString(sigHex)
	verify := func(pub ed25519.PublicKey, tag string, body []byte, s []byte) bool {
		return ed25519.Verify(pub, append([]byte(tag), body...), s)
	}
	pub := ed25519.PublicKey(k.public())
	if !verify(pub, w1TagReceipt, jcs, sig) {
		t.Fatal("a correctly signed receipt does not verify")
	}
	other, _, _, _ := signW1(k, w1TagReceipt, map[string]any{"tenant_id": "ten_a", "grant_id": "grt_2", "attempt": 1, "execution_state": "REJECTED_STALE_VERSION"})
	for name, ok := range map[string]bool{
		"another grant (replay)":     verify(pub, w1TagReceipt, other, sig),
		"the record-event tag":       verify(pub, w1TagEvent, jcs, sig),
		"another identity's key":     verify(ed25519.PublicKey(newW1Key("core-outcome", 1).public()), w1TagReceipt, jcs, sig),
		"another version's key":      verify(ed25519.PublicKey(newW1Key("core-execution", 2).public()), w1TagReceipt, jcs, sig),
		"a flipped signature bit":    verify(pub, w1TagReceipt, jcs, append([]byte{sig[0] ^ 1}, sig[1:]...)),
		"an HMAC-shaped 64-hex tail": verify(pub, w1TagReceipt, jcs, make([]byte, 64)),
	} {
		if ok {
			t.Errorf("%s verified; it must not", name)
		}
	}
}
