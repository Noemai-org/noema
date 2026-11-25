package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// A692 (interim, relayed; OD-P-119; REQ-EXE-006, REQ-AUD-006): the generated receipt reads a receipt that carries step_params and writes it back with the member, and
// still reads the old shape that has none. Fixtures are SYNTHETIC.
func receiptFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "fixtures", "valid", "execution-receipt", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAReceiptWithStepParamsIsReadAndWrittenBackWithThem(t *testing.T) {
	for _, name := range []string{"with_step_params.json", "step_params_every_scalar_kind.json"} {
		var r contracts.ExecutionReceiptV1
		if err := json.Unmarshal(receiptFixture(t, name), &r); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), `"step_params"`) {
			t.Fatalf("%s: the generated receipt dropped step_params: %s", name, out)
		}
	}
}

func TestAReceiptWithNoStepParamsIsStillReadAndWritesNone(t *testing.T) {
	var r contracts.ExecutionReceiptV1
	if err := json.Unmarshal(receiptFixture(t, "acknowledged.json"), &r); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "step_params") {
		t.Fatalf("an old receipt gained step_params: %s", out)
	}
}
