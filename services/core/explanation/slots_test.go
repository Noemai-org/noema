package explanation_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// EXP-01 slice 4, ruling A155: the {fact:<kind>} slot syntax in pack templates (REQ-EXP-002, INV-13, AGENT-17; PERD 31.2). SYNTHETIC facts and templates; the pack itself is
// not edited by this slice (that edit needs one Founder approval), so these tests write their own template files.

const (
	outcome = "fin.outcome.exception.status_explained"
	class   = "SUCCESS_CONFIRMED"
	tplKey  = outcome + "/" + class
)

// slotTemplates writes an en-NG template file holding the given texts under the key outcome/class and loads it.
func slotTemplates(t *testing.T, text string) explanation.Templates {
	t.Helper()
	dir := t.TempDir()
	raw, _ := json.Marshal(map[string]string{tplKey: text})
	if err := os.WriteFile(filepath.Join(dir, "en-NG.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	tpl, err := explanation.LoadTemplates(dir, []string{"en-NG"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return tpl
}

func withFacts(items ...contracts.ExplanationFactsV2FactsItem) contracts.ExplanationFactsV2 {
	f := facts(outcome, class)
	f.CorrelationId = "corr_t1"
	f.Facts = items
	return f
}

func fact(id, kind, status, value string) contracts.ExplanationFactsV2FactsItem {
	return contracts.ExplanationFactsV2FactsItem{FactId: id, Kind: kind, FactStatus: contracts.ExplanationFactsV2FactsItemFactStatus(status), ValueMasked: value, SourceLabel: "SYNTHETIC ledger"}
}

const known = "KNOWN"

// A155: a slot is filled from the value of the one KNOWN fact of its kind, and the result passes the validator by itself.
func TestASlotIsFilledFromTheOneKnownFactOfItsKind(t *testing.T) {
	tpl := slotTemplates(t, "Your transfer of {fact:payment.amount} to the account {fact:account} has settled.")
	f := withFacts(fact("f1", "payment.amount", known, "NGN 5,000"), fact("f2", "account", known, "ending 0001"))
	r, err := explanation.Fallback(tpl, f)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Your transfer of NGN 5,000 to the account ending 0001 has settled."; r.Text != want {
		t.Fatalf("text %q, want %q", r.Text, want)
	}
	if r.TemplateID != "en-NG/"+tplKey || len(r.Claims) != 1 || r.Claims[0].TemplateID != r.TemplateID || r.Claims[0].Span != r.Text {
		t.Fatalf("the whole text must be one template-bound claim: %+v", r)
	}
	if strings.Join(r.FactIDs, ",") != "f1,f2" {
		t.Fatalf("fact ids %v, want f1,f2 in slot order", r.FactIDs)
	}
	if problems := explanation.Validate(tpl, r, f, []string{`(?i)guarantee`}); len(problems) != 0 {
		t.Fatalf("a correctly filled slot fails the validator: %v", problems)
	}
}

// A slot that cannot be filled from exactly one KNOWN fact is an error, never a guess: none, two of the kind, INFERRED, empty, or a value that carries a brace.
func TestASlotThatCannotBeFilledFromOneKnownFactIsRefused(t *testing.T) {
	tpl := slotTemplates(t, "Your transfer of {fact:payment.amount} has settled.")
	cases := map[string]contracts.ExplanationFactsV2{
		"no fact of the kind":    withFacts(fact("f1", "account", known, "ending 0001")),
		"two facts of the kind":  withFacts(fact("f1", "payment.amount", known, "NGN 5,000"), fact("f2", "payment.amount", known, "NGN 6,000")),
		"an inferred fact":       withFacts(fact("f1", "payment.amount", "INFERRED", "NGN 5,000")),
		"an empty value":         withFacts(fact("f1", "payment.amount", known, "")),
		"a value with a brace":   withFacts(fact("f1", "payment.amount", known, "NGN {fact:account}")),
		"a value with a closing": withFacts(fact("f1", "payment.amount", known, "NGN 5}")),
	}
	for name, f := range cases {
		r, err := explanation.Fallback(tpl, f)
		if !errors.Is(err, explanation.ErrSlot) {
			t.Errorf("%s: got %v, want ErrSlot", name, err)
		}
		if r.Text != "" {
			t.Errorf("%s: rendered %q", name, r.Text)
		}
	}
}

// A template with a brace that is not a well-formed slot is refused at load: an unresolved placeholder must never reach a customer.
func TestATemplateWithAStrayBraceOrAMalformedSlotIsRefusedAtLoad(t *testing.T) {
	for name, text := range map[string]string{
		"a bare placeholder":   "Hello {name}, your transfer has settled.",
		"an empty kind":        "Your transfer of {fact:} has settled.",
		"an upper-case kind":   "Your transfer of {fact:Account} has settled.",
		"a spaced slot":        "Your transfer of { fact:account } has settled.",
		"an opening brace":     "Your transfer { has settled.",
		"a closing brace":      "Your transfer } has settled.",
		"a slot with a suffix": "Your transfer of {fact:account:x} has settled.",
	} {
		dir := t.TempDir()
		raw, _ := json.Marshal(map[string]string{tplKey: text})
		if err := os.WriteFile(filepath.Join(dir, "en-NG.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := explanation.LoadTemplates(dir, []string{"en-NG"}); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

// The validator recomputes the expansion from the facts it is given, so a rendered text that is not what the template and the KNOWN facts produce is blocked,
// whoever built it (A155; INV-13).
func TestTheValidatorRecomputesTheFilledTemplateFromTheFacts(t *testing.T) {
	tpl := slotTemplates(t, "Your transfer of {fact:payment.amount} has settled.")
	f := withFacts(fact("f1", "payment.amount", known, "NGN 5,000"))
	good, err := explanation.Fallback(tpl, f)
	if err != nil {
		t.Fatal(err)
	}
	altered := good
	altered.Text = "Your transfer of NGN 50,000 has settled."
	altered.Claims = []explanation.Claim{{Span: altered.Text, TemplateID: altered.TemplateID}}
	if problems := explanation.Validate(tpl, altered, f, nil); len(problems) == 0 {
		t.Error("a text with a value that is not the fact's passed")
	}
	reordered := good
	reordered.Text = "Your transfer has settled of NGN 5,000."
	reordered.Claims = []explanation.Claim{{Span: reordered.Text, TemplateID: reordered.TemplateID}}
	if problems := explanation.Validate(tpl, reordered, f, nil); len(problems) == 0 {
		t.Error("a text made of the right words and figures but not the template filled from the facts passed")
	}
	if problems := explanation.Validate(tpl, good, withFacts(fact("f1", "payment.amount", known, "NGN 6,000")), nil); len(problems) == 0 {
		t.Error("a text filled from other facts than the ones validated against passed")
	}
	if problems := explanation.Validate(tpl, good, withFacts(fact("f1", "payment.amount", "INFERRED", "NGN 5,000")), nil); len(problems) == 0 {
		t.Error("a slot filled from a fact that is not KNOWN passed")
	}
	if problems := explanation.Validate(tpl, good, withFacts(), nil); len(problems) == 0 {
		t.Error("a slot with no fact passed")
	}
}

// A brace in a rendered text is blocked whatever built it: a placeholder that was not filled is not an answer.
func TestABraceInARenderedTextIsBlocked(t *testing.T) {
	tpl := load(t)
	f := facts("fin.outcome.exception.status_explained", "SUCCESS_CONFIRMED")
	r, err := explanation.Fallback(tpl, f)
	if err != nil {
		t.Fatal(err)
	}
	r.Text += " {"
	r.Claims = append(r.Claims, explanation.Claim{Span: " {", FactIDs: []string{"f1"}})
	if problems := explanation.Validate(tpl, r, f, nil); len(problems) == 0 {
		t.Error("a text with an unfilled placeholder passed")
	}
}

// The pack's own templates carry no slot yet, so slice 3's behaviour is unchanged for them (the pack edit is a separate, Founder-approved change).
func TestTheTemplatesOfThePackStillRenderUnchanged(t *testing.T) {
	tpl := load(t)
	f := facts("fin.outcome.exception.status_explained", "SUCCESS_CONFIRMED")
	r, err := explanation.Fallback(tpl, f)
	if err != nil || len(r.FactIDs) != 0 || strings.ContainsAny(r.Text, "{}") {
		t.Fatalf("pack template: %+v, %v", r, err)
	}
}
