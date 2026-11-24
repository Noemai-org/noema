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
				if label == "missing" && name == "clarification" {
					// A593, A600: clarification.v1 requires none_option_id in every mode but SLOT, a conditional rule. The generated types model shape and leave conditional rules to the schema
					// validator (tools/codegen/README.md), so this type decodes the document; the refusal is held by tools/contracts/test_none_option.py and test_clarification_slot_mode.py.
					if err := json.Unmarshal(raw, c.into()); err != nil {
						t.Errorf("%s: a clarification with no none_option_id is a conditional rule of the schema, not of the type: %v", name, err)
					}
					continue
				}
				if err := json.Unmarshal(raw, c.into()); err == nil {
					t.Errorf("%s: a clarification whose none_option_id is %s was accepted", name, label)
				}
			}
		})
	}
}

// A582, A593 (mode SLOT): a SLOT clarification decodes with no candidate set, no options and no none choice, and writes back without inventing them; the rule that every other mode carries
// them is the schema's (a conditional rule), held by tools/contracts/test_clarification_slot_mode.py.
func TestASlotClarificationDecodesWithNoCandidateSetAndNoNoneChoice(t *testing.T) {
	var c contracts.ClarificationV1
	doc, err := decodeFixture(t, "clarification", "slot_when.json", &c)
	if err != nil {
		t.Fatalf("the SLOT fixture is refused: %v", err)
	}
	if c.Mode == nil || *c.Mode != contracts.ClarificationV1ModeSLOT || c.CandidateSetId != nil || c.CandidateSetVersion != nil || c.NoneOptionId != nil || c.AllowNone != nil || c.Attribute != nil {
		t.Errorf("decoded %+v", c)
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"candidate_set_id", "candidate_set_version", "none_option_id", "allow_none", "attribute"} {
		if _, ok := back[k]; ok {
			t.Errorf("the SLOT card written back carries %s", k)
		}
	}
	if back["slot"] != doc["slot"] || back["question_key"] != doc["question_key"] {
		t.Errorf("the SLOT card lost its slot or its question key: %v", back)
	}
}
