package payloadguard_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

// A259, A279/A270, REQ-SEC-003: the guard gives the verdict of the one shared vector file, contracts/fixtures/digit_run_vectors.json, which the model service's redactor and the
// UI scan read as well. This class is the source of truth for the digit run; the file ties the three consumers together. All data is SYNTHETIC.

type digitRunVector struct {
	Name      string `json:"name"`
	Group     string `json:"group"`
	Proves    string `json:"proves"`
	Input     string `json:"input"`
	Refused   bool   `json:"refused"`
	Exemption string `json:"exemption"`
}

func loadDigitRunVectors(t *testing.T) []digitRunVector {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "fixtures", "digit_run_vectors.json"))
	if err != nil {
		t.Fatalf("the shared vector file cannot be read: %v", err)
	}
	var f struct {
		Vectors []digitRunVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("the shared vector file is not JSON: %v", err)
	}
	if len(f.Vectors) < 100 {
		t.Fatalf("only %d vectors: the test is reading the wrong file", len(f.Vectors))
	}
	return f.Vectors
}

func TestTheSharedVectorsAreNamedOnceAndSaySomething(t *testing.T) {
	seen := map[string]bool{}
	groups := map[string]int{}
	for _, v := range loadDigitRunVectors(t) {
		if v.Name == "" || v.Proves == "" || v.Group == "" {
			t.Errorf("a vector has no name, group or claim: %+v", v.Name)
		}
		if seen[v.Name] {
			t.Errorf("the vector name %q is used twice", v.Name)
		}
		seen[v.Name] = true
		groups[v.Group]++
	}
	for _, g := range []string{"length", "separator_joins", "ends_a_run", "separators_in_a_row", "scripts", "comma", "money_accepted", "money_plain", "money_refused"} {
		if groups[g] == 0 {
			t.Errorf("the vector file has no vector in the group %q", g)
		}
	}
}

// Every vector, by name: the guard refuses the string exactly when the file says refused, as the value at a path with no exemption of its own.
func TestCheckGivesTheVerdictOfEverySharedVector(t *testing.T) {
	for _, v := range loadDigitRunVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]string{"v": v.Input})
			if err != nil {
				t.Fatal(err)
			}
			got := payloadguard.Check(payload) != nil
			if got != v.Refused {
				t.Errorf("%s: refused = %v, the file says %v (%s)", v.Name, got, v.Refused, v.Proves)
			}
		})
	}
}

// Mask has no path and no money exemption (the rulings name the guard, the redactor and the UI scan): for every vector the exemption does not decide, Mask changes the string
// exactly when the guard refuses it, because both use digitRun (A194).
func TestMaskAgreesWithTheSharedVectorsThatNeedNoExemption(t *testing.T) {
	for _, v := range loadDigitRunVectors(t) {
		if v.Exemption != "" {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			if changed := payloadguard.Mask(v.Input) != v.Input; changed != v.Refused {
				t.Errorf("%s: Mask changes the string = %v, the file says refused = %v", v.Name, changed, v.Refused)
			}
		})
	}
}
