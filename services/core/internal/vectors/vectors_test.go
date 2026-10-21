// Package vectors_test runs the cross-language golden vectors (contracts/fixtures/golden_vectors.json, CON-04)
// through the Go implementations. It is test-only because the pure packages may not read files.
package vectors_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

type vectors struct {
	JCS []struct {
		Name           string `json:"name"`
		ExpectedJCS    string `json:"expected_jcs"`
		ExpectedSHA256 string `json:"expected_sha256"`
		Input          any    `json:"input"`
	} `json:"jcs_vectors"`
	Idem []struct {
		Name  string `json:"name"`
		Input struct {
			TenantID      string `json:"tenant_id"`
			PrincipalID   string `json:"principal_id"`
			TransactionID string `json:"transaction_id"`
			LogicalAction string `json:"logical_action"`
			ReasonCode    string `json:"reason_code"`
			PolicyScope   string `json:"policy_scope"`
			Epoch         string `json:"logical_resolution_epoch"`
		}
		Expected string `json:"expected_idempotency_key"`
	} `json:"idempotency_key_vectors"`
	Grants []struct {
		Name        string         `json:"name"`
		KeyID       string         `json:"key_id"`
		SecretHex   string         `json:"secret_hex"`
		Grant       map[string]any `json:"grant"`
		ExpectedJCS string         `json:"expected_jcs"`
		ExpectedSig struct {
			KeyID string `json:"key_id"`
			Value string `json:"value"`
		} `json:"expected_sig"`
	} `json:"grant_signing_vectors"`
}

func load(t *testing.T) vectors {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/fixtures/golden_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // numbers stay integers; a float64 would be refused by crypto
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// CON-04: JCS and its hash agree with the vectors.
func TestJCSVectors(t *testing.T) {
	for _, c := range load(t).JCS {
		got, err := crypto.CanonicalJSON(c.Input)
		if err != nil || string(got) != c.ExpectedJCS || crypto.Hash(got) != c.ExpectedSHA256 {
			t.Errorf("%s: %s %v", c.Name, got, err)
		}
	}
}

// REQ-EXE-002: the idempotency key vectors.
func TestIdempotencyKeyVectors(t *testing.T) {
	for _, c := range load(t).Idem {
		in := c.Input
		got := crypto.IdempotencyKey(crypto.IdempotencyKeyFields{TenantID: in.TenantID, PrincipalID: in.PrincipalID, TransactionID: in.TransactionID,
			LogicalAction: in.LogicalAction, ReasonCode: in.ReasonCode, PolicyScope: in.PolicyScope, LogicalResolutionEpoch: in.Epoch})
		if got != c.Expected {
			t.Errorf("%s: %s want %s", c.Name, got, c.Expected)
		}
	}
}

// REQ-TRU-008, REQ-EXE-001: the grant signing vector, and a tampered grant is refused.
func TestGrantSigningVector(t *testing.T) {
	for _, c := range load(t).Grants {
		secret, _ := hex.DecodeString(c.SecretHex)
		ring := crypto.NewKeyRing(map[string][]byte{c.KeyID: secret})
		canon, err := crypto.CanonicalJSON(c.Grant)
		if err != nil || string(canon) != c.ExpectedJCS {
			t.Fatalf("%s: %s %v", c.Name, canon, err)
		}
		sig, _ := ring.SignWith(c.KeyID, canon)
		if sig != c.ExpectedSig.Value || !ring.VerifyWith(c.KeyID, canon, sig) {
			t.Fatalf("%s: signature %s", c.Name, sig)
		}
		c.Grant["plan_step"] = json.Number("3")
		tampered, _ := crypto.CanonicalJSON(c.Grant)
		if ring.VerifyWith(c.KeyID, tampered, sig) {
			t.Fatal("a tampered grant must not verify")
		}
		if ring.VerifyWith("other-key", canon, sig) {
			t.Fatal("an unknown key must not verify")
		}
	}
}

func TestFloatsAndHugeIntegersAreRefused(t *testing.T) {
	for _, v := range []any{map[string]any{"a": 1.5}, map[string]any{"a": 2.5e6}, map[string]any{"a": int64(9007199254740992)}} {
		if _, err := crypto.CanonicalJSON(v); err == nil {
			t.Errorf("%v must be refused", v)
		}
	}
}
