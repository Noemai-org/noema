package controller_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// PERD v1.18 REQ-EXE-004 and the founder's decision of 28 Oct (EXE-04): the Controller verifies the receipt's HMAC before it
// hands the receipt to the Ledger, for the receipt of a dispatch and for the sweeper's receipt. A signature that is empty,
// not hex, tampered or of another message is refused, and a failed verification writes nothing.

// forgingKeys signs as the honest ring does and then replaces the signature.
type forgingKeys struct {
	inner controller.KeyVerifier
	forge func(keyID string, real string) string
}

func (f forgingKeys) VerifyWith(keyID string, msg []byte, sig string) bool {
	return f.inner.VerifyWith(keyID, msg, sig)
}

func (f forgingKeys) SignWith(keyID string, msg []byte) (string, error) {
	real, err := f.inner.SignWith(keyID, msg)
	return f.forge(keyID, real), err
}

func forgeries(honest controller.KeyVerifier) map[string]func(keyID, real string) string {
	return map[string]func(keyID, real string) string{
		"an empty signature":             func(string, string) string { return "" },
		"64 characters that are not hex": func(string, string) string { return strings.Repeat("zz", 32) },
		"a tampered signature (last char moved)": func(_, real string) string {
			return real[:63] + map[bool]string{true: "1", false: "0"}[real[63] == '0']
		},
		"a signature of 63 hex characters":     func(_, real string) string { return real[:63] },
		"the upper case of the real signature": func(_, real string) string { return strings.ToUpper(real) },
		"a valid signature of another message": func(keyID, _ string) string {
			other, _ := honest.SignWith(keyID, []byte("another document"))
			return other
		},
	}
}

func TestAReceiptWithAForgedOrEmptySignatureIsNeverWritten(t *testing.T) {
	for name := range forgeries(NewWorld([]byte("synthetic-simulation-key-not-a-secret")).Keys) {
		t.Run(name, func(t *testing.T) {
			w, c, g := setup(t)
			c.Keys = forgingKeys{inner: w.Keys, forge: func(id, real string) string {
				return forgeries(w.Keys)[name](id, real)
			}}
			_, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
			if !errors.Is(err, controller.ErrReceiptSignature) {
				t.Fatalf("Execute: %v, want ErrReceiptSignature", err)
			}
			s := w.Snapshot()
			for _, e := range s.Record {
				if e.Type == "RECEIPT" {
					t.Fatalf("a RECEIPT event was written: %+v", e)
				}
			}
			if len(s.Receipts) != 0 || s.Pending != 1 || len(s.Dispatches) != 1 {
				t.Fatalf("receipts %d, attempts without a receipt %d, dispatches %d; want 0, 1 and 1", len(s.Receipts), s.Pending, len(s.Dispatches))
			}
			// The sweeper has the same check: with the forging signer it writes nothing either.
			if n, err := c.Recover(ctx(t), TenantID); !errors.Is(err, controller.ErrReceiptSignature) || n != 0 {
				t.Fatalf("Recover with a forging signer: %d, %v; want 0 and ErrReceiptSignature", n, err)
			}
			if len(w.Snapshot().Receipts) != 0 {
				t.Fatal("the sweeper wrote a receipt with a forged signature")
			}
			// With the honest ring the attempt is taken over as OUTCOME_UNKNOWN, for Outcome to verify.
			c.Keys = w.Keys
			if n, err := c.Recover(ctx(t), TenantID); err != nil || n != 1 {
				t.Fatalf("Recover with the honest ring: %d, %v; want 1", n, err)
			}
			for _, r := range w.Snapshot().Receipts {
				if r.State != controller.StateOutcomeUnknown {
					t.Fatalf("receipt state %s, want OUTCOME_UNKNOWN", r.State)
				}
			}
		})
	}
}
