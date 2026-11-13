package reconstruct

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func num(n int) json.Number { return json.Number(strings.TrimSpace(string(rune('0' + n)))) }

func receiptStory(mutate func([]stored) []stored) []stored {
	g := "grt_synth0001"
	evs := []stored{
		{id: "recev_01", eventType: evGrantIssued, objectRef: g, at: "2026-11-02T09:00:01Z", payload: map[string]any{"grant_id": g, "capability_id": "test.cap.create_case", "expires_at": "2026-11-02T10:00:00Z"}},
		{id: "recev_02", eventType: evGrantSpent, objectRef: g, at: "2026-11-02T09:00:02Z", payload: map[string]any{"grant_id": g, "attempt": num(1)}},
		{id: "recev_03", eventType: evReceipt, objectRef: g, at: "2026-11-02T09:00:03Z", payload: map[string]any{"grant_id": g, "attempt": num(1), "logical_action": "test.cap.create_case", "execution_state": "ACKNOWLEDGED"}},
	}
	if mutate != nil {
		evs = mutate(evs)
	}
	return evs
}

// reconstruction.v3.json, ACTION grant_state: derived by core from the Record, never asserted by a caller. Each
// non-VALID value is a breach of INV-03 that the reconstruction must show.
func TestGrantStateIsDerivedFromTheRecord(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]stored) []stored
		want   string
	}{
		{"issued, spent once, in time, same capability", nil, grantValid},
		{"never issued", func(e []stored) []stored { return e[1:] }, grantMissing},
		{"issued but never spent for this attempt", func(e []stored) []stored { return []stored{e[0], e[2]} }, grantUnmatched},
		{"spent twice", func(e []stored) []stored {
			dup := e[1]
			dup.id = "recev_02b"
			return []stored{e[0], dup, e[1], e[2]}
		}, grantSpent},
		{"spent after it expired", func(e []stored) []stored {
			e[1].at = "2026-11-02T10:00:00Z"
			return e
		}, grantExpired},
		{"issued for another capability", func(e []stored) []stored {
			e[0].payload["capability_id"] = "test.cap.other_case"
			return e
		}, grantUnmatched},
		{"voided before it was spent", func(e []stored) []stored {
			void := stored{id: "recev_00v", eventType: evGrantVoided, objectRef: e[0].objectRef, payload: map[string]any{}}
			return []stored{e[0], void, e[1], e[2]}
		}, grantUnmatched},
		{"an issue for a different grant does not count", func(e []stored) []stored {
			e[0].objectRef = "grt_synth0002"
			return e
		}, grantMissing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			evs := receiptStory(c.mutate)
			if got := grantState(evs, len(evs)-1); got != c.want {
				t.Fatalf("grant_state %s, want %s", got, c.want)
			}
		})
	}
}

// Facts outside the contract are refused, never served (reconstruction.v3.json $defs/Item, PERD 32.2).
func TestFactsOutsideTheContractAreRefused(t *testing.T) {
	bad := map[string]any{
		"decision_outcome":  "MAYBE",
		"execution_state":   "SUCCEEDED",
		"verdict":           "DONE",
		"capability_id":     "ESCALATE_CASE",
		"resolved_target":   "TX0001",
		"classification":    "settled",
		"candidate_count":   json.Number("21"),
		"attempt":           json.Number("0"),
		"text":              "",
		"disclosed_refs":    []any{"not a ref"},
		"bound_fact_status": "TRUE",
		"anything":          json.Number("-1"),
		"Not_Snake":         "x",
		"long":              strings.Repeat("x", maxFactString+1),
		"nested":            map[string]any{"a": "b"},
	}
	for k, v := range bad {
		if _, err := fact(k, v, nil); err == nil {
			t.Errorf("fact %s = %v was accepted", k, v)
		}
	}
	if _, err := fact("bound_fact_status", nil, nil); err != nil {
		t.Errorf("a claim bound to no fact (null) is itself a finding and must be servable: %v", err)
	}
}

// The closed sets equal the contract's enums (contracts/schemas/enums.v1.json).
func TestSetsEqualTheContractEnums(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "schemas", "enums.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for name, set := range map[string]map[string]bool{"DecisionOutcome": decisionOutcomes, "ExecutionState": executionStates, "Verdict": verdicts, "FactStatus": factStatuses} {
		var have []string
		for k := range set {
			have = append(have, k)
		}
		want := append([]string(nil), doc.Defs[name].Enum...)
		sort.Strings(have)
		sort.Strings(want)
		if !reflect.DeepEqual(have, want) {
			t.Errorf("%s: core has %v, the contract has %v", name, have, want)
		}
	}
}

// Every projected event type is a Record event type of PERD 32.2, and every section is one of the twelve.
func TestTheProjectionTableUsesOnlyContractNames(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "schemas", "record-event.v3.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, e := range doc.Defs["RecordEventType"].Enum {
		types[e] = true
	}
	sections := map[string]bool{}
	for _, s := range sectionOrder {
		sections[s] = true
	}
	for et, p := range projections {
		if !types[et] {
			t.Errorf("%s is not a record-event.v3 event type", et)
		}
		if !sections[p.section] {
			t.Errorf("%s goes to %s, which is not a section", et, p.section)
		}
	}
	if len(sectionOrder) != 12 {
		t.Fatalf("%d sections, want 12", len(sectionOrder))
	}
}
