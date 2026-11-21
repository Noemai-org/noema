package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// A275 (REQ-AMB-004, PERD section 39.2; AGENT-15): the clarification and the clarification card carry the core-minted none_option_id, which the UI submits as option_id to answer
// "None of these". Both generated types read it, write it back, and refuse an object without it or with a member that is not a string. The generated Go types do not check patterns
// (tools/codegen/README.md), so the form of the ID is held by tools/contracts/test_none_option.py and the Pydantic models; that it differs from every option_id of the same
// clarification is not shape and is checked by tools/contracts/validate.py. All data is SYNTHETIC.

func decodeFixture(t *testing.T, folder, file string, into any) (map[string]any, error) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, "valid", folder, file))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc, json.Unmarshal(raw, into)
}

func TestTheClarificationAndTheCardCarryTheCoreMintedNoneOptionID(t *testing.T) {
	cases := map[string]struct {
		folder, file string
		into         func() any
	}{
		"clarification": {"clarification", "two_options.json", func() any { return new(contracts.ClarificationV1) }},
		"card":          {"overlay-card", "clarification_four_options.json", func() any { return new(contracts.OverlayCardV1ClarificationCard) }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			into := c.into()
			doc, err := decodeFixture(t, c.folder, c.file, into)
			if err != nil {
				t.Fatalf("the valid fixture is refused: %v", err)
			}
			out, err := json.Marshal(into)
			if err != nil {
				t.Fatal(err)
			}
			var back map[string]any
			if err := json.Unmarshal(out, &back); err != nil {
				t.Fatal(err)
			}
			if back["none_option_id"] != doc["none_option_id"] || doc["none_option_id"] == nil {
				t.Fatalf("none_option_id = %v after a round trip, want %v", back["none_option_id"], doc["none_option_id"])
			}
			for label, mutate := range map[string]func(map[string]any){
				"missing":      func(m map[string]any) { delete(m, "none_option_id") },
				"not a string": func(m map[string]any) { m["none_option_id"] = 5 },
				"null":         func(m map[string]any) { m["none_option_id"] = nil },
			} {
				bad := map[string]any{}
				for k, v := range doc {
					bad[k] = v
				}
				mutate(bad)
				raw, _ := json.Marshal(bad)
				if err := json.Unmarshal(raw, c.into()); err == nil {
					t.Errorf("%s: a clarification whose none_option_id is %s was accepted", name, label)
				}
			}
		})
	}
}
