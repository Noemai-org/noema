package crypto_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// Found by the independent validation of the pgsodium spike (docs#117): Go's and OpenSSL's Ed25519 verifiers accept a signature under a
// small-order public key for any message; libsodium refuses it. A verifier here never trusts such a key, nor a non-canonical encoding.
func TestUsableEd25519PublicKey(t *testing.T) {
	good, _, err := ed25519.GenerateKey(rand.Reader) // a random key per run, SYNTHETIC
	if err != nil {
		t.Fatal(err)
	}
	if !crypto.UsableEd25519PublicKey(good) {
		t.Fatal("a freshly generated key was refused")
	}
	if crypto.UsableEd25519PublicKey(good[:31]) || crypto.UsableEd25519PublicKey(append(append([]byte{}, good...), 0)) || crypto.UsableEd25519PublicKey(nil) {
		t.Fatal("a key of the wrong length was accepted")
	}
	bad := map[string]string{
		"the identity":          "01" + strings.Repeat("00", 31),
		"order 2":               "ec" + strings.Repeat("ff", 30) + "7f",
		"y = 0, sign 0":         strings.Repeat("00", 32),
		"y = 0, sign 1":         strings.Repeat("00", 31) + "80",
		"order 8 (a), sign 0":   "26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05",
		"order 8 (a), sign 1":   "26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc85",
		"order 8 (b), sign 0":   "c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac037a",
		"order 8 (b), sign 1":   "c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac03fa",
		"identity as y = p + 1": "ee" + strings.Repeat("ff", 30) + "7f",
		"y = p":                 "ed" + strings.Repeat("ff", 30) + "7f",
		"y = 2^255 - 1":         strings.Repeat("ff", 31) + "7f",
		"y = 2^255 - 1, sign 1": strings.Repeat("ff", 32),
	}
	for name, h := range bad {
		k, err := hex.DecodeString(h)
		if err != nil || len(k) != 32 {
			t.Fatalf("%s: bad test encoding", name)
		}
		if crypto.UsableEd25519PublicKey(k) {
			t.Errorf("%s was accepted", name)
		}
	}
}
