package registry_test

// A363, A364, A369 (interim, relayed, not yet committed): the pack's members after the Founder's text of 19 Nov 2026. `write_when` is a member of the intent NODE; `may_write`,
// `write_when` on an evidence source and `produced_by` are not members; `case_reasons` is a catalogue of {code, label?}. REQ-CAP-001, INV-03. Everything here is SYNTHETIC edits
// of the repository's own pack; the failure modes are malformed packs, never a mocked dependency.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/registry"
)

const investigate = "fin.payments.exception.investigate"

// editedPack reads the fintech pack, applies edit to its generic form and returns the bytes.
func editedPack(t *testing.T, edit func(p map[string]any)) []byte {
	t.Helper()
	b, err := os.ReadFile(fintechPack)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var p map[string]any
	if err := dec.Decode(&p); err != nil {
		t.Fatal(err)
	}
	edit(p)
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func node(p map[string]any, id string) map[string]any {
	for _, n := range p["intent_nodes"].([]any) {
		if m := n.(map[string]any); m["node_id"] == id {
			return m
		}
	}
	return nil
}

func loadErr(t *testing.T, raw []byte) *registry.Error {
	t.Helper()
	_, err := registry.Load(bytes.NewReader(raw))
	var e *registry.Error
	if !errors.As(err, &e) {
		t.Fatalf("the pack loaded or failed untyped, want a registry error: %v", err)
	}
	return e
}

func TestTheFintechPackSaysWhenTheInvestigateNodeNeedsAWrite(t *testing.T) {
	editedPack(t, func(p map[string]any) {
		const want = "question_type == 'ESCALATION_REQUEST' or requested_remedy != null"
		if got := node(p, investigate)["write_when"]; got != want {
			t.Fatalf("write_when is %v, want %q", got, want)
		}
		for _, s := range p["evidence_sources"].([]any) {
			m := s.(map[string]any)
			if _, ok := m["may_write"]; ok {
				t.Fatalf("source %v carries may_write", m["source_id"])
			}
			if _, ok := m["write_when"]; ok {
				t.Fatalf("source %v carries write_when", m["source_id"])
			}
		}
		if len(p["case_reasons"].([]any)) == 0 {
			t.Fatal("the pack has no case_reasons")
		}
	})
}

// The generated types are closed: a member that is not in the schema does not load (fail closed).
func TestMembersThatAreNotInTheContractDoNotLoad(t *testing.T) {
	for name, edit := range map[string]func(p map[string]any){
		"may_write":              func(p map[string]any) { p["evidence_sources"].([]any)[0].(map[string]any)["may_write"] = true },
		"write_when on a source": func(p map[string]any) { p["evidence_sources"].([]any)[0].(map[string]any)["write_when"] = "true" },
		"produced_by": func(p map[string]any) {
			p["evidence_descriptors"].([]any)[0].(map[string]any)["produced_by"] = "fin.src.case_system"
		},
		"case_reason with an extra": func(p map[string]any) {
			p["case_reasons"] = []any{map[string]any{"code": "PAYMENT_STATUS_QUERY", "weight": json.Number("1")}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			if e := loadErr(t, editedPack(t, edit)); e.Code != registry.CodeIncomplete {
				t.Fatalf("got %s: %s", e.Code, e.Reason)
			}
		})
	}
}

func TestAMalformedWriteWhenOrCaseReasonsFailsTheLoad(t *testing.T) {
	for name, edit := range map[string]func(p map[string]any){
		"write_when bool":   func(p map[string]any) { node(p, investigate)["write_when"] = true },
		"write_when null":   func(p map[string]any) { node(p, investigate)["write_when"] = nil },
		"case_reasons null": func(p map[string]any) { p["case_reasons"] = nil },
		"case_reasons str":  func(p map[string]any) { p["case_reasons"] = []any{"A"} },
		"code number":       func(p map[string]any) { p["case_reasons"] = []any{map[string]any{"code": json.Number("1")}} },
	} {
		t.Run(name, func(t *testing.T) {
			if e := loadErr(t, editedPack(t, edit)); e.Code != registry.CodeIncomplete {
				t.Fatalf("got %s: %s", e.Code, e.Reason)
			}
		})
	}
}
