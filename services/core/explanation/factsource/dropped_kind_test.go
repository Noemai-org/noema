package factsource_test

// EXP-05 / A711 (interim, relayed) and A676: the recipient kind is dropped. No name leaves the candidates read, so the masking core holds no recipient, no pack template holds a
// recipient slot and the facts inventory lists no recipient row. REQ-EXP-001, REQ-SEC-003; INV-13, AGENT-13. SYNTHETIC.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A676 and A711: no name of any kind leaves the candidates read, so the masking core names, reads, masks and emits no recipient (candidate_test.go holds the candidate source to the
// same rule). The word is banned in the file, comments included, so the dropped kind cannot come back unseen.
func TestNoRecipientNameOrKindIsInTheSourceOrTheCore(t *testing.T) {
	for _, f := range []string{"factsource.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(b)), "counterparty") {
			t.Errorf("%s names the counterparty (A676, A711)", f)
		}
	}
}

func readTemplates(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "packs", "fintech", "templates")
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no pack templates under %s: %v", dir, err)
	}
	out := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, ".forbidden.json") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// A711: no pack template and no inventory row names the dropped recipient kind. A template that still holds the slot, or an inventory that still lists the kind, fails here.
func TestNoTemplateOrInventoryRowNamesTheDroppedKind(t *testing.T) {
	const dropped = "payment.counter" + "party"
	for key, text := range readTemplates(t) {
		if strings.Contains(text, dropped) {
			t.Errorf("template %s names %s", key, dropped)
		}
	}
	inv, err := os.ReadFile(filepath.Join("..", "facts-inventory.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(inv), dropped) {
		t.Errorf("facts-inventory.md still names %s", dropped)
	}
}
