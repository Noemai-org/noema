package mapping_test

// EXP-01 (A550, A556, A665, A706, A710): the evidence mapping, fin.map.v1@3, declares which source carries the version token of the target a write acts on, and which payload
// member says the read was complete. The token is read from the stored payload by the mapping and by nothing else (INV-17: a version token is never read from a request); a read that is not
// marked complete, or a payload with no usable token, gives no token and never a guess (INV-19). Written before the code: this file fails until TargetVersion, the `target_versions` member and
// the version 3 file exist. REQ-EVD, REQ-RES-001, REQ-CTX-001; INV-14, INV-17, INV-19, INV-21. SYNTHETIC data only.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

const tvExpected = "fin.map.v1@3"

// tvDoc is a version 3 mapping: one rule-set row and one target version declaration of the ledger source.
func tvDoc(rsID string, mutate func(string) string) []byte {
	doc := `{"mapping_id":"fin.map.v1","version":3,"label":"SYNTHETIC","ruleset":"` + rsID + `","note":"n","rows":[
 {"entity":"switch","source":"fin.src.switch","fields":{"state":{"path":"semantic_status","map":{"SUCCESS":"SUCCESSFUL"}}}}],
 "target_versions":[{"source":"fin.src.core_ledger","token":"version_token","complete":"complete"}]}`
	if mutate != nil {
		doc = mutate(doc)
	}
	return []byte(doc)
}

func parseTV(t *testing.T, mutate func(string) string) (*mapping.Mapping, error) {
	t.Helper()
	return mapping.Parse(tvDoc(ruleSet(t).ID(), mutate), ruleSet(t), sources(t), tvExpected, limits)
}

func mustTV(t *testing.T) *mapping.Mapping {
	t.Helper()
	m, err := parseTV(t, nil)
	if err != nil {
		t.Fatalf("a version 3 mapping with a target version declaration must load: %v", err)
	}
	return m
}

func TestATargetVersionDeclarationLoadsAndTheMappingNamesItsVersion(t *testing.T) {
	if m := mustTV(t); m.ID() != tvExpected {
		t.Errorf("ID = %q, want %q", m.ID(), tvExpected)
	}
}

func TestATargetVersionDeclarationThatIsNotWholeIsRefusedAtLoad(t *testing.T) {
	if _, err := parseTV(t, nil); err != nil {
		t.Fatalf("control: the unmutated document must load, or every refusal below proves nothing: %v", err)
	}
	const decl = `{"source":"fin.src.core_ledger","token":"version_token","complete":"complete"}`
	bad := map[string]func(string) string{
		"an empty list": func(s string) string {
			return strings.Replace(s, `"target_versions":[`+decl+`]`, `"target_versions":[]`, 1)
		},
		"a source that is not the pack's": func(s string) string {
			return strings.Replace(s, `"source":"fin.src.core_ledger","token"`, `"source":"fin.src.ghost","token"`, 1)
		},
		"no source":                func(s string) string { return strings.Replace(s, `"source":"fin.src.core_ledger",`, ``, 1) },
		"no token member":          func(s string) string { return strings.Replace(s, `"token":"version_token",`, ``, 1) },
		"an empty token member":    func(s string) string { return strings.Replace(s, `"token":"version_token"`, `"token":""`, 1) },
		"no complete marker":       func(s string) string { return strings.Replace(s, `,"complete":"complete"}]`, `}]`, 1) },
		"an empty complete marker": func(s string) string { return strings.Replace(s, `"complete":"complete"}]`, `"complete":""}]`, 1) },
		"the marker is the token": func(s string) string {
			return strings.Replace(s, `"complete":"complete"}]`, `"complete":"version_token"}]`, 1)
		},
		"a member the loader does not know": func(s string) string {
			return strings.Replace(s, `"complete":"complete"}]`, `"complete":"complete","default":"v0"}]`, 1)
		},
		"the same source twice": func(s string) string {
			return strings.Replace(s, `"target_versions":[`+decl+`]`, `"target_versions":[`+decl+`,`+decl+`]`, 1)
		},
		"not a list": func(s string) string {
			return strings.Replace(s, `"target_versions":[`+decl+`]`, `"target_versions":`+decl, 1)
		},
		"a repeated member": func(s string) string {
			return strings.Replace(s, `"target_versions":[`+decl+`]`, `"target_versions":[`+decl+`],"target_versions":[`+decl+`]`, 1)
		},
	}
	for name, mutate := range bad {
		if m, err := parseTV(t, mutate); err == nil || m != nil {
			t.Errorf("%s: the mapping loaded; it must be refused whole", name)
		}
	}
}

func TestATargetVersionIsTheTokenOfAPayloadMarkedComplete(t *testing.T) {
	m := mustTV(t)
	got, err := m.TargetVersion(ledger, map[string]any{"version_token": "12", "complete": true, "entries": []any{}})
	if err != nil || got != "12" {
		t.Errorf("TargetVersion = %q, %v; want \"12\", nil", got, err)
	}
	if got, err := m.TargetVersion(ledger, map[string]any{"version_token": "v7-a.b", "complete": true}); err != nil || got != "v7-a.b" {
		t.Errorf("the token is opaque text, not a number: %q, %v", got, err)
	}
}

// INV-19: a read that is not marked complete is not an answer. The marker is the boolean true and nothing else, and it is looked at before the token, so a token on an incomplete read is never used.
func TestAPayloadThatIsNotMarkedCompleteGivesNoVersion(t *testing.T) {
	m := mustTV(t)
	for name, payload := range map[string]map[string]any{
		"no marker":                 {"version_token": "12"},
		"marker false":              {"version_token": "12", "complete": false},
		"marker a string":           {"version_token": "12", "complete": "true"},
		"marker a number":           {"version_token": "12", "complete": json.Number("1")},
		"marker null":               {"version_token": "12", "complete": nil},
		"marker false and no token": {"complete": false},
	} {
		got, err := m.TargetVersion(ledger, payload)
		if !errors.Is(err, mapping.ErrIncomplete) || got != "" {
			t.Errorf("%s: got %q, %v; want no token and ErrIncomplete", name, got, err)
		}
	}
}

func TestAPayloadWithNoUsableTokenGivesNoVersion(t *testing.T) {
	m := mustTV(t)
	for name, payload := range map[string]map[string]any{
		"no token member":        {"complete": true},
		"a null token":           {"version_token": nil, "complete": true},
		"a number token":         {"version_token": json.Number("12"), "complete": true},
		"an int token":           {"version_token": 12, "complete": true},
		"a boolean token":        {"version_token": true, "complete": true},
		"an empty token":         {"version_token": "", "complete": true},
		"a token with a newline": {"version_token": "1\n2", "complete": true},
		"a token with a NUL":     {"version_token": "1\x002", "complete": true},
		"a list token":           {"version_token": []any{"12"}, "complete": true},
		"an empty payload":       {},
		"a nil payload":          nil,
	} {
		got, err := m.TargetVersion(ledger, payload)
		if err == nil || got != "" {
			t.Errorf("%s: got %q, %v; want no token and an error", name, got, err)
		}
		if errors.Is(err, mapping.ErrIncomplete) && name != "an empty payload" && name != "a nil payload" {
			t.Errorf("%s: the read is complete, so the error must not say it is not: %v", name, err)
		}
	}
}

func TestASourceTheMappingDoesNotDeclareHasNoTargetVersion(t *testing.T) {
	m := mustTV(t)
	payload := map[string]any{"version_token": "12", "complete": true}
	for _, src := range []string{switchSrc, destination, "fin.src.ghost", ""} {
		if got, err := m.TargetVersion(src, payload); !errors.Is(err, mapping.ErrNoTargetVersion) || got != "" {
			t.Errorf("source %q: got %q, %v; want ErrNoTargetVersion", src, got, err)
		}
	}
}

// A mapping with no `target_versions` member (every version below 3) declares none: no source gives a token, and nothing is defaulted.
func TestAMappingWithNoDeclarationGivesNoVersionForAnySource(t *testing.T) {
	m, err := mapping.Parse(tvDoc2(ruleSet(t).ID()), ruleSet(t), sources(t), "fin.map.v1@2", limits)
	if err != nil {
		t.Fatalf("a mapping with no declaration must still load: %v", err)
	}
	if got, err := m.TargetVersion(ledger, map[string]any{"version_token": "12", "complete": true}); !errors.Is(err, mapping.ErrNoTargetVersion) || got != "" {
		t.Errorf("got %q, %v; want ErrNoTargetVersion", got, err)
	}
}

func tvDoc2(rsID string) []byte {
	return []byte(`{"mapping_id":"fin.map.v1","version":2,"label":"SYNTHETIC","ruleset":"` + rsID + `","note":"n","rows":[
 {"entity":"switch","source":"fin.src.switch","fields":{"state":{"path":"semantic_status","map":{"SUCCESS":"SUCCESSFUL"}}}}]}`)
}

// The declaration is not a row: it adds no reconciliation row and no list entity (the rule set has no entity for a token).
func TestATargetVersionDeclarationAddsNoRow(t *testing.T) {
	m := mustTV(t)
	if rows, err := m.Rows(ledger, map[string]any{"version_token": "12", "complete": true}); err != nil || len(rows) != 0 {
		t.Errorf("the ledger gives rows %v, %v; the declaration is not a row", rows, err)
	}
	if _, err := m.ListRows(ledger, "ledger", map[string]any{"complete": true}); !errors.Is(err, mapping.ErrNoSuchList) {
		t.Errorf("the declaration is not a list entity: %v", err)
	}
}

// --- the real pack data ----------------------------------------------------------------------------------------------------------------------

func TestTheRealMappingIsVersion3AndDeclaresTheLedgersTargetVersion(t *testing.T) {
	m, err := mapping.Parse(realFile(t), ruleSet(t), sources(t), tvExpected, limits, mapping.WithScope(packScope(t)))
	if err != nil {
		t.Fatalf("the pack's mapping must be %s and load with the pack's own scope: %v", tvExpected, err)
	}
	if m.ID() != tvExpected {
		t.Errorf("ID = %q", m.ID())
	}
	if got, err := m.TargetVersion(ledger, map[string]any{"version_token": "12", "complete": true}); err != nil || got != "12" {
		t.Errorf("the real mapping's ledger token: %q, %v", got, err)
	}
	if _, err := m.TargetVersion(ledger, map[string]any{"version_token": "12"}); !errors.Is(err, mapping.ErrIncomplete) {
		t.Errorf("the real mapping reads no token from a payload not marked complete: %v", err)
	}
	for _, src := range []string{switchSrc, destination, caseSystem} {
		if _, err := m.TargetVersion(src, map[string]any{"version_token": "12", "complete": true}); !errors.Is(err, mapping.ErrNoTargetVersion) {
			t.Errorf("only the ledger declares a target version; %s: %v", src, err)
		}
	}
}
