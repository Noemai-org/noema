package explanation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// EXP-01 slice 3 (rulings A8, A18; REQ-EXP-002, INV-13, AGENT-17; PERD 31.2). SYNTHETIC facts. The renderer here is the deterministic fallback: no model, no network.

const packTemplates = "../../../packs/fintech/templates"

var locales = []string{"en-NG"}

func facts(outcome, class string) contracts.ExplanationFactsV2 {
	return contracts.ExplanationFactsV2{Locale: contracts.ExplanationFactsV2LocaleEnNG, OutcomeId: contracts.CommonV1PackId(outcome), Classification: class,
		Facts: []contracts.ExplanationFactsV2FactsItem{{FactId: "f1", Kind: "payment.amount", FactStatus: contracts.ExplanationFactsV2FactsItemFactStatusKNOWN, ValueMasked: "NGN 5,000", SourceLabel: "SYNTHETIC ledger"}}}
}

func load(t *testing.T) explanation.Templates {
	t.Helper()
	tpl, err := explanation.LoadTemplates(packTemplates, locales)
	if err != nil {
		t.Fatal(err)
	}
	return tpl
}

// REQ-EXP-002: a fallback exists for every outcome and classification the pack can produce, and each one passes the validator by itself.
func TestEveryPackTemplateRendersAndPassesTheValidator(t *testing.T) {
	tpl := load(t)
	keys := tpl.Keys("en-NG")
	if len(keys) == 0 {
		t.Fatal("no en-NG template loaded")
	}
	for _, key := range keys {
		outcome, class, _ := strings.Cut(key, "/")
		f := facts(outcome, class)
		r, err := explanation.Fallback(tpl, f)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if r.TemplateID != "en-NG/"+key || !strings.HasPrefix(r.TemplateVersion, "sha256:") || r.Text == "" {
			t.Fatalf("%s: rendered %+v", key, r)
		}
		if problems := explanation.Validate(r, f, nil); len(problems) != 0 {
			t.Fatalf("%s: the pack's own fallback fails the validator: %v", key, problems)
		}
	}
}

// A18 / REQ-EXP-003: a locale or key with no template fails closed. There is no silent fallback to another locale here.
func TestAMissingLocaleOrKeyFailsClosed(t *testing.T) {
	tpl := load(t)
	f := facts("fin.outcome.exception.status_explained", "SUCCESS_CONFIRMED")
	f.Locale = contracts.ExplanationFactsV2LocalePcm
	if _, err := explanation.Fallback(tpl, f); err == nil {
		t.Fatal("a locale with no template rendered")
	}
	g := facts("fin.outcome.exception.status_explained", "NO_SUCH_CLASS")
	if _, err := explanation.Fallback(tpl, g); err == nil {
		t.Fatal("a classification with no template rendered")
	}
	if _, err := explanation.LoadTemplates(packTemplates, []string{"xx-XX"}); err == nil {
		t.Fatal("a declared locale with no file loaded")
	}
}

// An empty template file entry fails closed at load, not at render.
func TestAnEmptyTemplateTextIsRefusedAtLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "en-NG.json"), []byte(`{"o/C": "  "}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := explanation.LoadTemplates(dir, []string{"en-NG"}); err == nil {
		t.Fatal("an empty template was loaded")
	}
}

func good(f contracts.ExplanationFactsV2) explanation.Rendered {
	return explanation.Rendered{Text: "Your payment of NGN 5,000 is confirmed.", TemplateID: "en-NG/x/y", Claims: []explanation.Claim{{Span: "Your payment of NGN 5,000 is confirmed.", FactIDs: []string{"f1"}}}}
}

// PERD 31.2 checks 1 to 4. Each fails on its own input, and the clean case passes (so the checks are not vacuous).
func TestTheValidatorBlocksEachKindOfInventedOrUnboundClaim(t *testing.T) {
	f := facts("o", "c")
	if p := explanation.Validate(good(f), f, nil); len(p) != 0 {
		t.Fatalf("a bound, fact-only text was blocked: %v", p)
	}
	cases := map[string]explanation.Rendered{
		"an amount that is not in the facts": {Text: "Your payment of NGN 9,000 is confirmed.", Claims: []explanation.Claim{{Span: "Your payment of NGN 9,000 is confirmed.", FactIDs: []string{"f1"}}}},
		"a time that is not in the facts":    {Text: "Your payment of NGN 5,000 is confirmed. It will arrive by 14:30.", Claims: []explanation.Claim{{Span: "Your payment of NGN 5,000 is confirmed.", FactIDs: []string{"f1"}}, {Span: "It will arrive by 14:30.", FactIDs: []string{"f1"}}}},
		"a sentence no claim covers":         {Text: "Your payment of NGN 5,000 is confirmed. Everything is fine.", Claims: []explanation.Claim{{Span: "Your payment of NGN 5,000 is confirmed.", FactIDs: []string{"f1"}}}},
		"a claim bound to a fact that does not exist": {Text: "Your payment of NGN 5,000 is confirmed.", Claims: []explanation.Claim{{Span: "Your payment of NGN 5,000 is confirmed.", FactIDs: []string{"nope"}}}},
		"a claim with neither a fact nor a template":  {Text: "Your payment of NGN 5,000 is confirmed.", Claims: []explanation.Claim{{Span: "Your payment of NGN 5,000 is confirmed."}}},
		"a claim whose span is not in the text":       {Text: "Your payment of NGN 5,000 is confirmed.", Claims: []explanation.Claim{{Span: "Something else.", FactIDs: []string{"f1"}}}},
		"an unmasked long digit run":                  {Text: "Account 0123456789 is confirmed.", Claims: []explanation.Claim{{Span: "Account 0123456789 is confirmed.", FactIDs: []string{"f1"}}}},
	}
	for name, r := range cases {
		if p := explanation.Validate(r, f, nil); len(p) == 0 {
			t.Errorf("%s was not blocked", name)
		}
	}
}

func TestAForbiddenClaimIsBlockedEvenWhenItIsBoundToAFact(t *testing.T) {
	f := facts("o", "c")
	r := explanation.Rendered{Text: "Your money is safe.", Claims: []explanation.Claim{{Span: "Your money is safe.", FactIDs: []string{"f1"}}}}
	if p := explanation.Validate(r, f, nil); len(p) != 0 {
		t.Fatalf("with no pattern list the sentence is only checked for binding: %v", p)
	}
	if p := explanation.Validate(r, f, []string{`(?i)money is safe`}); len(p) == 0 {
		t.Fatal("a forbidden claim passed")
	}
}

// A template claim is bound by its template ID, which must be the rendered template's own.
func TestATemplateClaimMustNameTheRenderedTemplate(t *testing.T) {
	f := facts("o", "c")
	r := explanation.Rendered{Text: "We could not confirm it.", TemplateID: "en-NG/o/c", Claims: []explanation.Claim{{Span: "We could not confirm it.", TemplateID: "en-NG/o/c"}}}
	if p := explanation.Validate(r, f, nil); len(p) != 0 {
		t.Fatalf("a template-bound sentence was blocked: %v", p)
	}
	r.Claims[0].TemplateID = "en-NG/other/key"
	if p := explanation.Validate(r, f, nil); len(p) == 0 {
		t.Fatal("a claim bound to another template passed")
	}
}
