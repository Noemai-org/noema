package registry_test

// A342 and A345 (interim, relayed, not yet committed): `may_write` on an evidence source and `produced_by` on an evidence
// descriptor. REQ-CAP-001, INV-05, INV-14. Everything here is SYNTHETIC edits of the repository's own pack; the failure modes
// are malformed packs, never a mocked dependency.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/registry"
)

const caseSystem = "fin.src.case_system"

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

func sources(p map[string]any) []any { return p["evidence_sources"].([]any) }

func sourceByID(t *testing.T, p map[string]any, id string) map[string]any {
	t.Helper()
	for _, s := range sources(p) {
		if m := s.(map[string]any); m["source_id"] == id {
			return m
		}
	}
	t.Fatalf("no source %s", id)
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

// The pack says which source may be written to: the case system and no other.
func TestTheFintechPackMarksOnlyTheCaseSystemAsMayWrite(t *testing.T) {
	editedPack(t, func(p map[string]any) {
		var writable []string
		for _, s := range sources(p) {
			m := s.(map[string]any)
			if v, ok := m["may_write"]; ok && v == true {
				writable = append(writable, m["source_id"].(string))
			}
		}
		if len(writable) != 1 || writable[0] != caseSystem {
			t.Fatalf("may_write sources are %v, want only %s", writable, caseSystem)
		}
	})
}

// Safer reading: absent is false, so a write capability whose verification source does not say may_write fails the load.
func TestAWriteCapabilityOnASourceWithoutMayWriteFailsTheLoad(t *testing.T) {
	raw := editedPack(t, func(p map[string]any) { delete(sourceByID(t, p, caseSystem), "may_write") })
	e := loadErr(t, raw)
	if e.Code != registry.CodeSafetyBlock || !strings.Contains(e.Reason, "is not may_write") {
		t.Fatalf("got %s: %s, want INTERNAL_SAFETY_BLOCK naming the source as not may_write", e.Code, e.Reason)
	}
}

func TestAWriteCapabilityOnASourceMarkedMayWriteFalseFailsTheLoad(t *testing.T) {
	raw := editedPack(t, func(p map[string]any) { sourceByID(t, p, caseSystem)["may_write"] = false })
	e := loadErr(t, raw)
	if e.Code != registry.CodeSafetyBlock || !strings.Contains(e.Reason, "is not may_write") {
		t.Fatalf("got %s: %s, want INTERNAL_SAFETY_BLOCK naming the source as not may_write", e.Code, e.Reason)
	}
}

// Fail closed: a value that is not a boolean never loads (a regression guard: the generated type already refuses it).
func TestAMalformedMayWriteFailsTheLoad(t *testing.T) {
	for name, v := range map[string]any{"string": "yes", "null": nil, "number": json.Number("1"), "array": []any{true}} {
		t.Run(name, func(t *testing.T) {
			raw := editedPack(t, func(p map[string]any) { sourceByID(t, p, caseSystem)["may_write"] = v })
			if e := loadErr(t, raw); e.Code != registry.CodeIncomplete {
				t.Fatalf("got %s: %s", e.Code, e.Reason)
			}
		})
	}
}

// A capability that reads, and a prohibited one, need no may_write on their source.
func TestOnlyAllowedWriteCapabilitiesNeedMayWrite(t *testing.T) {
	raw := editedPack(t, func(p map[string]any) {
		for _, c := range p["capabilities"].([]any) {
			m := c.(map[string]any)
			if m["risk_class"] != "R0_read" && m["risk_class"] != "R0_internal" && m["prohibited"] != true {
				m["verification"].(map[string]any)["source"] = "fin.src.customer"
			}
		}
	})
	if e := loadErr(t, raw); e.Code != registry.CodeSafetyBlock || !strings.Contains(e.Reason, "fin.src.customer") {
		t.Fatalf("got %s: %s", e.Code, e.Reason)
	}
}

func TestProducedByMustNameASourceOfThePack(t *testing.T) {
	raw := editedPack(t, func(p map[string]any) {
		p["evidence_descriptors"].([]any)[0].(map[string]any)["produced_by"] = "fin.src.nope"
	})
	e := loadErr(t, raw)
	if e.Code != registry.CodeIncomplete || !strings.Contains(e.Reason, "not an evidence source of the pack") {
		t.Fatalf("got %s: %s, want a refusal naming produced_by as not an evidence source of the pack", e.Code, e.Reason)
	}
}

func TestProducedByNamingASourceOfThePackLoads(t *testing.T) {
	raw := editedPack(t, func(p map[string]any) {
		for _, d := range p["evidence_descriptors"].([]any) {
			m := d.(map[string]any)
			m["produced_by"] = m["source"]
		}
	})
	if _, err := registry.Load(bytes.NewReader(raw)); err != nil {
		t.Fatalf("produced_by naming the descriptor's own source did not load: %v", err)
	}
}
