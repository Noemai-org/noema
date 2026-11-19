package registry_test

// A364 (interim, relayed, not yet committed): `write_when` on an evidence source says WHEN a capability may write to it, as a predicate of the pack's own language; absent means
// never. REQ-CAP-001, INV-03, INV-05. Everything here is SYNTHETIC edits of the repository's own pack; the failure modes are malformed packs, never a mocked dependency.

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

// The pack says which source may be written to, and when: the case system and no other.
func TestTheFintechPackSetsWriteWhenOnlyOnTheCaseSystem(t *testing.T) {
	editedPack(t, func(p map[string]any) {
		var writable []string
		for _, s := range sources(p) {
			m := s.(map[string]any)
			if v, ok := m["write_when"]; ok && v != "" {
				writable = append(writable, m["source_id"].(string))
			}
			if _, ok := m["may_write"]; ok {
				t.Fatalf("source %v still carries may_write", m["source_id"])
			}
		}
		if len(writable) != 1 || writable[0] != caseSystem {
			t.Fatalf("write_when sources are %v, want only %s", writable, caseSystem)
		}
	})
}

// Safer reading: absent is never, so a write capability whose verification source has no write_when fails the load.
func TestAWriteCapabilityOnASourceWithoutWriteWhenFailsTheLoad(t *testing.T) {
	raw := editedPack(t, func(p map[string]any) { delete(sourceByID(t, p, caseSystem), "write_when") })
	e := loadErr(t, raw)
	if e.Code != registry.CodeSafetyBlock || !strings.Contains(e.Reason, "has no write_when") {
		t.Fatalf("got %s: %s, want INTERNAL_SAFETY_BLOCK naming the source as having no write_when", e.Code, e.Reason)
	}
}

func TestAnEmptyWriteWhenIsNoWriteWhen(t *testing.T) {
	raw := editedPack(t, func(p map[string]any) { sourceByID(t, p, caseSystem)["write_when"] = "" })
	e := loadErr(t, raw)
	if e.Code != registry.CodeSafetyBlock || !strings.Contains(e.Reason, "has no write_when") {
		t.Fatalf("got %s: %s", e.Code, e.Reason)
	}
}

// Fail closed: a value that is not a string never loads (a regression guard: the generated type already refuses it).
func TestAMalformedWriteWhenFailsTheLoad(t *testing.T) {
	for name, v := range map[string]any{"bool": true, "null": nil, "number": json.Number("1"), "array": []any{"true"}} {
		t.Run(name, func(t *testing.T) {
			raw := editedPack(t, func(p map[string]any) { sourceByID(t, p, caseSystem)["write_when"] = v })
			if e := loadErr(t, raw); e.Code != registry.CodeIncomplete {
				t.Fatalf("got %s: %s", e.Code, e.Reason)
			}
		})
	}
}

// may_write and produced_by are gone (A364, A363): a pack that still carries either does not load.
func TestMayWriteAndProducedByAreNotMembersOfThePack(t *testing.T) {
	for name, edit := range map[string]func(p map[string]any){
		"may_write": func(p map[string]any) { sourceByID(t, p, caseSystem)["may_write"] = true },
		"produced_by": func(p map[string]any) {
			p["evidence_descriptors"].([]any)[0].(map[string]any)["produced_by"] = caseSystem
		},
	} {
		t.Run(name, func(t *testing.T) {
			if e := loadErr(t, editedPack(t, edit)); e.Code != registry.CodeIncomplete {
				t.Fatalf("got %s: %s", e.Code, e.Reason)
			}
		})
	}
}

// A capability that reads, and a prohibited one, need no write_when on their source.
func TestOnlyAllowedWriteCapabilitiesNeedWriteWhen(t *testing.T) {
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
