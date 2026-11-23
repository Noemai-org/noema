package intakeadapters

// TRU-14, ruling A511 (interim; the Planner): the `applies_when` of an action graph is a predicate of the SoT 43.7 language, evaluated by the Trust policy expression evaluator
// (internal/predicate) under the pack's own vocabulary, with no second language. This test pins that for every action graph of the shipped pack: each `applies_when` parses and
// type-checks as a boolean with the same scope the policy's conditions are checked with. If a graph ever needs a form the evaluator does not read, this test fails and the grammar
// differs: that is a CONFLICT DETECTED to raise (A511), not a reason to add a language. REQ-TRU-004, REQ-PLN-002; INV-14. The pack is the shipped fintech pack.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

func TestEveryActionGraphsAppliesWhenIsAPredicateOfTheTrustEvaluatorUnderThePacksVocabulary(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packs", "fintech_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	vocab, err := vocabulary(top)
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "packs", "fintech", "graph", "g*.json"))
	if err != nil || len(files) < 3 {
		t.Fatalf("%d action graphs found (%v), want the pack's three", len(files), err)
	}
	sort.Strings(files)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var g struct {
			GraphID     string `json:"graph_id"`
			AppliesWhen string `json:"applies_when"`
		}
		if err := json.Unmarshal(raw, &g); err != nil || g.AppliesWhen == "" {
			t.Fatalf("%s: %v, applies_when %q", f, err, g.AppliesWhen)
		}
		prog, err := predicate.Compile(g.AppliesWhen, vocab.Scope)
		if err != nil || prog == nil {
			t.Errorf("%s (%s): applies_when %q is not a predicate of the Trust evaluator under the pack's vocabulary: %v (A511: a different grammar is a CONFLICT to raise)", f, g.GraphID, g.AppliesWhen, err)
		}
	}
}
