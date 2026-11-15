package understanding_test

// UND-04, core half, ruling A166 (items 1 and 3; REQ-INT-001, REQ-INP-002; INV-12, INV-29): the model service never signs and never sets principal_type or
// asserted_by. The gate takes the two identity members from the session and refuses a signature the model made; a normal compile raises no override. SYNTHETIC.

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/understanding"
)

// Ruling A166 item 3: the model's output carries no principal_type and no asserted_by. The contract still requires the two members, so the model service fills them with
// the marker "unset" and the core writes the session's values; that is not an override and raises nothing. Any other value, even the session's own, is a shape error.
func TestAnIdentityMemberTheModelServiceLeftUnsetIsTheSessionsAndIsNotAnOverride(t *testing.T) {
	a, err := gate(t).Admit(raw(t, nil), session, corr)
	if err != nil {
		t.Fatal(err)
	}
	p := a.Intent.Object().Principal
	if p.PrincipalType != session.PrincipalType || p.AssertedBy != session.AssertedBy {
		t.Errorf("admitted principal_type %q asserted_by %q, want the session's %q %q", p.PrincipalType, p.AssertedBy, session.PrincipalType, session.AssertedBy)
	}
	if len(a.Overrides) != 0 {
		t.Errorf("a normal compile reported overrides %v: the caller would record a PRINCIPAL_OVERRIDE_ATTEMPT that did not happen", a.Overrides)
	}
	if strings.Contains(string(a.Intent.JSON()), `"unset"`) {
		t.Errorf("the marker survives in the canonical form the hash covers: %s", a.Intent.JSON())
	}
}

func TestAnIdentityMemberTheModelSetAnywayIsRefusedUnderItsOwnReason(t *testing.T) {
	for name, set := range map[string]func(p map[string]any){
		"principal_type, another value":     func(p map[string]any) { p["principal_type"] = "operator" },
		"principal_type, the session's":     func(p map[string]any) { p["principal_type"] = session.PrincipalType },
		"asserted_by, another value":        func(p map[string]any) { p["asserted_by"] = "model" },
		"asserted_by, the session's":        func(p map[string]any) { p["asserted_by"] = session.AssertedBy },
		"principal_type, the marker, cased": func(p map[string]any) { p["principal_type"] = "UNSET" },
		"asserted_by, the marker and more":  func(p map[string]any) { p["asserted_by"] = "unset " },
		"principal_type, empty":             func(p map[string]any) { p["principal_type"] = "" },
	} {
		_, err := gate(t).Admit(raw(t, func(m map[string]any) { set(m["principal"].(map[string]any)) }), session, corr)
		if err == nil || !has(reasons(t, err), understanding.ReasonIdentitySetByModel) {
			t.Errorf("%s: %v, want an IDENTITY_SET_BY_MODEL refusal (ruling A195)", name, err)
			continue
		}
		if has(reasons(t, err), understanding.ReasonShape) {
			t.Errorf("%s: the refusal also carries SHAPE; the attempt must be told apart from a malformed document (ruling A195)", name)
		}
	}
}

// Ruling A195: only a value the model SET is the new reason. A member that is null or not a string is a malformed document: the closed decode refuses it first, as SHAPE,
// and it is not reported as an identity attempt.
func TestAnIdentityMemberThatIsNullOrNotAStringStaysAShapeError(t *testing.T) {
	for name, value := range map[string]any{"null": nil, "a number": 7, "an object": map[string]any{}, "a list": []any{}} {
		_, err := gate(t).Admit(raw(t, func(m map[string]any) { m["principal"].(map[string]any)["principal_type"] = value }), session, corr)
		if err == nil || !has(reasons(t, err), understanding.ReasonShape) || has(reasons(t, err), understanding.ReasonIdentitySetByModel) {
			t.Errorf("principal_type %s: %v, want SHAPE and not IDENTITY_SET_BY_MODEL", name, err)
		}
	}
}

// Ruling A166 item 1: the model service never signs. The gate admits only the neutral marker the model service's own gate writes; a signature of any other form is refused,
// so no signature the model made can reach the stored object or the hash.
func TestASignatureTheModelServiceMadeIsRefused(t *testing.T) {
	for name, sig := range map[string]map[string]any{
		"a real-looking signature":  {"key_id": "und-2026-10", "value": "c2lnbmF0dXJl"},
		"the marker's key, a value": {"key_id": "unsigned", "value": "c2lnbmF0dXJl"},
		"a key, the marker's value": {"key_id": "und-2026-10", "value": "unsigned"},
		"empty members":             {"key_id": "", "value": ""},
		"the marker, cased":         {"key_id": "UNSIGNED", "value": "UNSIGNED"},
	} {
		_, err := gate(t).Admit(raw(t, func(m map[string]any) { m["sig"] = sig }), session, corr)
		if err == nil || !has(reasons(t, err), understanding.ReasonSig) {
			t.Errorf("%s: %v, want a SIG refusal", name, err)
		}
	}
	if _, err := gate(t).Admit(raw(t, nil), session, corr); err != nil {
		t.Errorf("the marker itself: %v", err)
	}
}
