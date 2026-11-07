package signing_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/signing"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// EXE-07 (W3 of DEC-2026-11-01-receipt-and-record-signing, Proposed; REQ-AUD-006, REQ-SEC-005): Ed25519 signing in core-execution.
// Slice 1: the signer. Every key here is SYNTHETIC (AGENT-25): a seed made of one repeated byte, generated for these tests,
// never mounted in a deployment and never registered by the deploy step.

const identity = "core-execution"

// load reads a keyring with the rotation overlap of REQ-SEC-005, two.
func load(dir string) (*signing.Ring, error) {
	return signing.LoadRing(identity, dir, signing.Options{MaxActive: 2})
}

// syntheticSeed is a SYNTHETIC test seed: 32 copies of n. It is public, and it is not a secret.
func syntheticSeed(n byte) []byte {
	b := make([]byte, ed25519.SeedSize)
	for i := range b {
		b[i] = n
	}
	return b
}

func keyID(version int) string { return fmt.Sprintf("%s.ed25519.%d", identity, version) }

// writeKey writes a SYNTHETIC key file as the deploy step would: <key_id>.key, mode 0400, the seed as 64 hex characters.
func writeKey(t *testing.T, dir string, version int, seed []byte, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(dir, keyID(version)+".key")
	if err := os.WriteFile(p, []byte(hex.EncodeToString(seed)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func ring(t *testing.T, versions ...int) (*signing.Ring, string) {
	t.Helper()
	dir := t.TempDir()
	for _, v := range versions {
		writeKey(t, dir, v, syntheticSeed(byte(v)), 0o400)
	}
	r, err := load(dir)
	if err != nil {
		t.Fatalf("LoadRing: %v", err)
	}
	return r, dir
}

func publicOf(version int) ed25519.PublicKey {
	return ed25519.NewKeyFromSeed(syntheticSeed(byte(version))).Public().(ed25519.PublicKey)
}

func receiptDoc() map[string]any {
	return map[string]any{"schema_version": "1.0", "receipt_id": "rcp_synth01", "tenant_id": "ten_synth01", "grant_id": "grt_synth01",
		"attempt": int64(1), "execution_state": "ACKNOWLEDGED", "external_ack": "case_synth01"}
}

// D6: the message is a fixed domain tag followed by the RFC 8785 bytes of the document without sig. A signature verifies over
// exactly that, and the key ID names the identity and the version (D4).
func TestASignatureVerifiesOverTheDomainTagAndTheCanonicalBytes(t *testing.T) {
	r, _ := ring(t, 1)
	s, err := r.Sign(signing.ReceiptTag, receiptDoc())
	if err != nil {
		t.Fatal(err)
	}
	canon, err := crypto.CanonicalJSON(receiptDoc())
	if err != nil {
		t.Fatal(err)
	}
	if string(s.SignedJCS) != string(canon) {
		t.Fatalf("SignedJCS is not the canonical document:\n%s\n%s", s.SignedJCS, canon)
	}
	if s.KeyID != keyID(1) {
		t.Fatalf("key ID %q, want %q", s.KeyID, keyID(1))
	}
	if len(s.Value) != 128 || strings.ToLower(s.Value) != s.Value {
		t.Fatalf("sig.value must be 128 lower-case hex characters, got %d characters", len(s.Value))
	}
	sig, err := hex.DecodeString(s.Value)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(publicOf(1), append([]byte("noema/receipt/v1\n"), canon...), sig) {
		t.Fatal("the signature does not verify over the domain tag and the canonical bytes")
	}
	if !signing.Verify(publicOf(1), signing.ReceiptTag, canon, s.Value) {
		t.Fatal("signing.Verify refuses a genuine signature")
	}
}

// The tag stops a signature over one type of object from verifying as another; the key stops one power's signature from
// passing as another's; any changed byte of the document or the signature fails.
func TestASignatureDoesNotVerifyAsAnotherTypeAnotherKeyOrAnotherDocument(t *testing.T) {
	r, _ := ring(t, 1)
	s, err := r.Sign(signing.ReceiptTag, receiptDoc())
	if err != nil {
		t.Fatal(err)
	}
	if signing.Verify(publicOf(1), signing.RecordEventTag, s.SignedJCS, s.Value) {
		t.Fatal("a receipt signature verified as a Record event (cross-type confusion)")
	}
	if signing.Verify(publicOf(2), signing.ReceiptTag, s.SignedJCS, s.Value) {
		t.Fatal("a signature verified under another key")
	}
	for i := range s.SignedJCS {
		altered := append([]byte(nil), s.SignedJCS...)
		altered[i] ^= 1
		if signing.Verify(publicOf(1), signing.ReceiptTag, altered, s.Value) {
			t.Fatalf("a signature verified over a document with byte %d changed", i)
		}
	}
	for i := 0; i < len(s.Value); i++ {
		b := []byte(s.Value)
		if b[i] == '0' {
			b[i] = '1'
		} else {
			b[i] = '0'
		}
		if signing.Verify(publicOf(1), signing.ReceiptTag, s.SignedJCS, string(b)) {
			t.Fatalf("a signature with hex digit %d changed still verified", i)
		}
	}
	for _, bad := range []string{"", "zz", strings.Repeat("0", 128), s.Value[:126], s.Value + "00", strings.ToUpper(s.Value)} {
		if signing.Verify(publicOf(1), signing.ReceiptTag, s.SignedJCS, bad) {
			t.Fatalf("a malformed signature %q verified", bad)
		}
	}
}

// D5, REQ-SEC-005: two keys are active during a rotation and the signer always uses the newest; a third is refused.
func TestTheSignerUsesTheNewestKeyAndAtMostTwoAreActive(t *testing.T) {
	r, _ := ring(t, 2, 3)
	s, err := r.Sign(signing.ReceiptTag, receiptDoc())
	if err != nil {
		t.Fatal(err)
	}
	if s.KeyID != keyID(3) {
		t.Fatalf("signed with %s, want the newest, %s", s.KeyID, keyID(3))
	}
	if !signing.Verify(publicOf(3), signing.ReceiptTag, s.SignedJCS, s.Value) {
		t.Fatal("the newest key's signature does not verify")
	}
	pubs := r.PublicKeys()
	if len(pubs) != 2 || !pubs[keyID(2)].Equal(publicOf(2)) || !pubs[keyID(3)].Equal(publicOf(3)) {
		t.Fatalf("PublicKeys = %v, want the public halves of versions 2 and 3", pubs)
	}
	dir := t.TempDir()
	for _, v := range []int{1, 2, 3} {
		writeKey(t, dir, v, syntheticSeed(byte(v)), 0o400)
	}
	if _, err := load(dir); err == nil {
		t.Fatal("three active keys were accepted; at most two may be active")
	}
}

// Fail closed: nothing signs without a key, and a key that is not exactly what the deploy step writes is refused.
func TestAKeyThatIsNotExactlyWhatTheDeployStepWritesIsRefused(t *testing.T) {
	good := hex.EncodeToString(syntheticSeed(7))
	cases := []struct {
		name   string
		file   string
		body   string
		mode   os.FileMode
		wantOK bool
	}{
		{"the deploy step's file", keyID(1) + ".key", good + "\n", 0o400, true},
		{"readable by the group", keyID(1) + ".key", good + "\n", 0o440, false},
		{"readable by everyone", keyID(1) + ".key", good + "\n", 0o444, false},
		{"writable by the owner", keyID(1) + ".key", good + "\n", 0o600, false},
		{"another identity", "core-outcome.ed25519.1.key", good + "\n", 0o400, false},
		{"a version with a leading zero", identity + ".ed25519.01.key", good + "\n", 0o400, false},
		{"version zero", identity + ".ed25519.0.key", good + "\n", 0o400, false},
		{"another algorithm in the name", identity + ".rsa.1.key", good + "\n", 0o400, false},
		{"a seed that is too short", keyID(1) + ".key", good[:62] + "\n", 0o400, false},
		{"a seed that is not hex", keyID(1) + ".key", strings.Repeat("g", 64) + "\n", 0o400, false},
		{"upper-case hex", keyID(1) + ".key", strings.ToUpper(hex.EncodeToString(syntheticSeed(0xab))) + "\n", 0o400, false},
		{"an empty file", keyID(1) + ".key", "", 0o400, false},
		{"two seeds", keyID(1) + ".key", good + "\n" + good + "\n", 0o400, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, c.file)
			if err := os.WriteFile(p, []byte(c.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(p, c.mode); err != nil {
				t.Fatal(err)
			}
			_, err := load(dir)
			if c.wantOK != (err == nil) {
				t.Fatalf("LoadRing error = %v, want ok=%v", err, c.wantOK)
			}
			if err != nil && (strings.Contains(err.Error(), good) || strings.Contains(err.Error(), "ggggg")) {
				t.Fatalf("the error repeats the key file's contents: %v", err)
			}
		})
	}
	if _, err := load(t.TempDir()); err == nil {
		t.Fatal("an empty directory gave a signer; nothing may sign without a key")
	}
	if _, err := load(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing directory gave a signer")
	}
	dir := t.TempDir()
	writeKey(t, dir, 1, syntheticSeed(1), 0o400)
	if err := os.Symlink(filepath.Join(dir, keyID(1)+".key"), filepath.Join(dir, keyID(2)+".key")); err != nil {
		t.Fatal(err)
	}
	if _, err := load(dir); err == nil {
		t.Fatal("a symbolic link was accepted as a key file")
	}
	fifoDir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(fifoDir, keyID(1)+".key"), 0o400); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := load(fifoDir); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a FIFO was accepted as a key file")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a FIFO held the loader open")
	}
}

// D3: no private key enters a log, a trace, a fixture or an error. Every way to print a ring or a key is redacted.
func TestNoWayToPrintAKeyOrARingShowsKeyMaterial(t *testing.T) {
	r, dir := ring(t, 1, 2)
	seeds := []string{hex.EncodeToString(syntheticSeed(1)), hex.EncodeToString(syntheticSeed(2))}
	priv := hex.EncodeToString(ed25519.NewKeyFromSeed(syntheticSeed(2)))
	var sb strings.Builder
	h := slog.New(slog.NewTextHandler(&sb, nil))
	h.Info("ring", "ring", r)
	h2 := slog.New(slog.NewJSONHandler(&sb, nil))
	h2.Info("ring", "ring", r)
	out := strings.Join([]string{
		fmt.Sprintf("%v", r), fmt.Sprintf("%+v", r), fmt.Sprintf("%#v", r), fmt.Sprintf("%s", r), fmt.Sprintf("%v", *r),
		fmt.Sprintf("%v", r.Newest()), fmt.Sprintf("%+v", r.Newest()), fmt.Sprintf("%#v", r.Newest()), sb.String(),
	}, "\n")
	for _, secret := range append(seeds, priv, dir) {
		if strings.Contains(out, secret) {
			t.Fatalf("printing the ring or a key shows %q", secret[:8]+"...")
		}
	}
	if !strings.Contains(out, keyID(2)) {
		t.Fatal("the redacted form should still name the key ID")
	}
}

// D8: the bytes that are signed are the bytes the database will hold, so they must be canonical. A document that is not
// canonical, repeats a member name, or already carries sig is refused.
func TestOnlyCanonicalDocumentsWithoutADuplicateMemberOrASigAreSigned(t *testing.T) {
	r, _ := ring(t, 1)
	canon, err := crypto.CanonicalJSON(receiptDoc())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.SignJCS(signing.ReceiptTag, canon); err != nil {
		t.Fatalf("a canonical document was refused: %v", err)
	}
	bad := map[string]string{
		"not canonical (spaces)":       strings.Replace(string(canon), ",", ", ", 1),
		"not canonical (member order)": `{"b":1,"a":2}`,
		"a repeated member":            `{"a":1,"a":2}`,
		"a repeated member, nested":    `{"a":{"b":1,"b":2}}`,
		"a float":                      `{"a":1.5}`,
		"an exponent":                  `{"a":1e3}`,
		"already signed":               `{"a":1,"sig":{"key_id":"x","value":"y"}}`,
		"not an object":                `[1,2]`,
		"trailing data":                `{"a":1}{"b":2}`,
		"empty":                        ``,
	}
	for name, doc := range bad {
		if _, err := r.SignJCS(signing.ReceiptTag, []byte(doc)); err == nil {
			t.Errorf("%s: the document was signed", name)
		}
	}
	// A repeated member is refused for its own reason, with a message that says so (D8): jsonb would keep only the last.
	for _, doc := range []string{`{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `{"a":1,"a":1}`} {
		if _, err := r.SignJCS(signing.ReceiptTag, []byte(doc)); err == nil || !strings.Contains(err.Error(), "appears more than once") {
			t.Errorf("%s: want a refusal that names the repeated member, got %v", doc, err)
		}
	}
	withSig := receiptDoc()
	withSig["sig"] = map[string]any{"key_id": keyID(1), "value": strings.Repeat("0", 128)}
	if _, err := r.Sign(signing.ReceiptTag, withSig); err == nil {
		t.Error("a map that already carries sig was signed")
	}
	if _, err := r.Sign(signing.Tag("noema/other/v1\n"), receiptDoc()); err == nil {
		t.Error("an unknown domain tag was accepted")
	}
}

// Ed25519 is deterministic: the same key and document give the same signature, so a replayed receipt is byte for byte the same.
func TestSigningIsDeterministic(t *testing.T) {
	r, _ := ring(t, 1)
	a, err1 := r.Sign(signing.ReceiptTag, receiptDoc())
	b, err2 := r.Sign(signing.ReceiptTag, receiptDoc())
	if err1 != nil || err2 != nil || a.Value != b.Value || a.KeyID != b.KeyID {
		t.Fatalf("two signatures of one document differ: %v %v", err1, err2)
	}
}

// The rotation overlap is policy, supplied by the caller: it must be at least one, and it bounds the keys mounted.
func TestTheRotationOverlapIsSuppliedAndBoundsTheMountedKeys(t *testing.T) {
	dir := t.TempDir()
	writeKey(t, dir, 1, syntheticSeed(1), 0o400)
	writeKey(t, dir, 2, syntheticSeed(2), 0o400)
	if _, err := signing.LoadRing(identity, dir, signing.Options{}); err == nil {
		t.Error("an overlap of zero was accepted")
	}
	if _, err := signing.LoadRing(identity, dir, signing.Options{MaxActive: 1}); err == nil {
		t.Error("two keys were accepted with an overlap of one")
	}
	if _, err := signing.LoadRing(identity, dir, signing.Options{MaxActive: 2}); err != nil {
		t.Errorf("two keys were refused with an overlap of two: %v", err)
	}
}

// D3: through the loader, a key file owned by another user is refused. Changing the owner needs privilege, so the test
// skips where chown is not permitted; the owner rule itself is unit-tested in owner_internal_test.go on every runner.
func TestLoadRingRefusesAKeyFileOwnedByAnotherUser(t *testing.T) {
	dir := t.TempDir()
	writeKey(t, dir, 1, syntheticSeed(1), 0o400)
	if err := os.Chown(filepath.Join(dir, keyID(1)+".key"), os.Geteuid()+1, -1); err != nil {
		t.Skipf("cannot change the owner here: %v", err)
	}
	if _, err := load(dir); err == nil {
		t.Fatal("a key file owned by another user was accepted")
	}
}
