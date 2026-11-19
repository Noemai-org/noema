package reconstruct

// A259, A308, A331, REQ-AUD-003, REQ-SEC-003: the projection serves a fact by the one digit definition of record/payloadguard. The money exemption is part of it only for customer-visible
// text (A331, relayed, interim): a stored `NGN 12,345.00` is served as it is by the `text` fact of a claim and as `NGN 12,*45.00` by any other fact. Every vector of the shared file
// contracts/fixtures/digit_run_vectors.json is served by a plain scalar fact, changed exactly when the file says refused or the string is a money string that only the exemption
// accepts; the money vectors are served as they are by the `text` fact. All values are SYNTHETIC.

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
			Name      string `json:"name"`
			Input     string `json:"input"`
			Refused   bool   `json:"refused"`
			Exemption string `json:"exemption"`
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
			if changed := got != v.Input; changed != (v.Refused || v.Exemption == "money") {
				t.Errorf("served %q for %q: changed = %v, the file says refused = %v with exemption %q", got, v.Input, changed, v.Refused, v.Exemption)
			}
			if v.Exemption == "money" {
				if text, err := fact("text", v.Input, nil); err != nil || text != v.Input {
					t.Errorf("the text fact served %v (%v) for the money string %q, want it as it is", text, err, v.Input)
				}
			}
		})
	}
}

// A331: a stored amount is customer-visible text only in a claim's text; anywhere else it is a string like any other.
func TestAStoredAmountIsServedAsItIsInTheClaimTextOnly(t *testing.T) {
	for _, amount := range []string{"NGN 12,345.00", "NGN 123,456,789.00"} {
		if got, err := fact("text", amount, nil); err != nil || got != amount {
			t.Errorf("the text fact served %v (%v) for %q", got, err, amount)
		}
		if got, err := fact("amount_text", amount, nil); err != nil || got == amount {
			t.Errorf("a plain scalar fact served %v (%v) for %q, want it masked", got, err, amount)
		}
	}
	if got, _ := fact("amount_text", "NGN 12345.00", nil); got != "NGN ***45.00" {
		t.Errorf("an ungrouped amount is masked like any other run: served %v", got)
	}
}
