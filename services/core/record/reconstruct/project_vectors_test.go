package reconstruct

// A259, A308, REQ-AUD-003, REQ-SEC-003: the projection serves a fact by the one digit definition of record/payloadguard, and the money exemption is part of it: a stored `NGN 12,345.00` is
// served as it is, not `NGN 12,*45.00`. Every vector of the shared file contracts/fixtures/digit_run_vectors.json is served by a plain scalar fact: changed exactly when the file says refused.
// All values are SYNTHETIC.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAPlainScalarFactIsServedByTheVerdictOfEverySharedVector(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "fixtures", "digit_run_vectors.json"))
	if err != nil {
		t.Fatalf("the shared vector file cannot be read: %v", err)
	}
	var f struct {
		Vectors []struct {
			Name    string `json:"name"`
			Input   string `json:"input"`
			Refused bool   `json:"refused"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &f); err != nil || len(f.Vectors) < 100 {
		t.Fatalf("the shared vector file is not the expected one: %v (%d vectors)", err, len(f.Vectors))
	}
	for _, v := range f.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := fact("amount_text", v.Input, nil)
			if err != nil {
				t.Fatal(err)
			}
			if changed := got != v.Input; changed != v.Refused {
				t.Errorf("served %q for %q: changed = %v, the file says refused = %v", got, v.Input, changed, v.Refused)
			}
		})
	}
}

func TestAStoredAmountIsServedAsItIs(t *testing.T) {
	for _, amount := range []string{"NGN 12,345.00", "NGN 123,456,789.00"} {
		if got, err := fact("amount_text", amount, nil); err != nil || got != amount {
			t.Errorf("fact served %v (%v) for %q", got, err, amount)
		}
	}
	if got, _ := fact("amount_text", "NGN 12345.00", nil); got != "NGN ***45.00" {
		t.Errorf("an ungrouped amount is masked like any other run: served %v", got)
	}
}
