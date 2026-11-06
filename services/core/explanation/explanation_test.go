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
		if problems := explanation.Validate(tpl, r, f, nil); len(problems) != 0 {
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
	tpl := load(t)
	f := facts("o", "c")
	if p := explanation.Validate(tpl, good(f), f, nil); len(p) != 0 {
		t.Fatalf("a bound, fact-only text was blocked: %v", p)
	}
	cases := map[string]explanation.Rendered{
		"an amount that is not in the facts":          {Text: "Your payment of NGN 9,000 is confirmed.", Claims: []explanation.Claim{{Span: "Your payment of NGN 9,000 is confirmed.", FactIDs: []string{"f1"}}}},
		"a time that is not in the facts":             {Text: "Your payment of NGN 5,000 is confirmed. It will arrive by 14:30.", Claims: []explanation.Claim{{Span: "Your payment of NGN 5,000 is confirmed.", FactIDs: []string{"f1"}}, {Span: "It will arrive by 14:30.", FactIDs: []string{"f1"}}}},
		"a sentence no claim covers":                  {Text: "Your payment of NGN 5,000 is confirmed. Everything is fine.", Claims: []explanation.Claim{{Span: "Your payment of NGN 5,000 is confirmed.", FactIDs: []string{"f1"}}}},
		"a claim bound to a fact that does not exist": {Text: "Your payment of NGN 5,000 is confirmed.", Claims: []explanation.Claim{{Span: "Your payment of NGN 5,000 is confirmed.", FactIDs: []string{"nope"}}}},
		"a claim with neither a fact nor a template":  {Text: "Your payment of NGN 5,000 is confirmed.", Claims: []explanation.Claim{{Span: "Your payment of NGN 5,000 is confirmed."}}},
		"a claim whose span is not in the text":       {Text: "Your payment of NGN 5,000 is confirmed.", Claims: []explanation.Claim{{Span: "Something else.", FactIDs: []string{"f1"}}}},
		"an unmasked long digit run":                  {Text: "Account 0123456789 is confirmed.", Claims: []explanation.Claim{{Span: "Account 0123456789 is confirmed.", FactIDs: []string{"f1"}}}},
	}
	for name, r := range cases {
		if p := explanation.Validate(tpl, r, f, nil); len(p) == 0 {
			t.Errorf("%s was not blocked", name)
		}
	}
}

func TestAForbiddenClaimIsBlockedEvenWhenItIsBoundToAFact(t *testing.T) {
	tpl := load(t)
	f := facts("o", "c")
	r := explanation.Rendered{Text: "Your money is safe.", Claims: []explanation.Claim{{Span: "Your money is safe.", FactIDs: []string{"f1"}}}}
	if p := explanation.Validate(tpl, r, f, nil); len(p) != 0 {
		t.Fatalf("with no pattern list the sentence is only checked for binding: %v", p)
	}
	if p := explanation.Validate(tpl, r, f, []string{`(?i)money is safe`}); len(p) == 0 {
		t.Fatal("a forbidden claim passed")
	}
}

// A template claim is bound by its template ID, which must be the rendered template's own, and its span must be the pack's text: a model cannot put its own sentence
// under a template's name.
func TestATemplateClaimMustNameTheRenderedTemplateAndQuoteIt(t *testing.T) {
	tpl := load(t)
	f := facts("fin.outcome.exception.status_explained", "INSUFFICIENT_EVIDENCE")
	r, err := explanation.Fallback(tpl, f)
	if err != nil {
		t.Fatal(err)
	}
	if p := explanation.Validate(tpl, r, f, nil); len(p) != 0 {
		t.Fatalf("a template-bound sentence was blocked: %v", p)
	}
	other := r
	other.Claims = []explanation.Claim{{Span: r.Text, TemplateID: "en-NG/fin.outcome.exception.case_opened/INSUFFICIENT_EVIDENCE"}}
	if p := explanation.Validate(tpl, other, f, nil); len(p) == 0 {
		t.Fatal("a claim bound to another template passed")
	}
	smuggled := explanation.Rendered{Text: r.Text + " Your refund of NGN 5,000 is on its way.", TemplateID: r.TemplateID, TemplateVersion: r.TemplateVersion}
	smuggled.Claims = []explanation.Claim{{Span: smuggled.Text, TemplateID: r.TemplateID}}
	if p := explanation.Validate(tpl, smuggled, f, nil); len(p) == 0 {
		t.Fatal("a model sentence under a template's name passed")
	}
}

// PERD 31.2 check 4 on its own: a long digit run is blocked even when it appears in the facts, so only masked values may carry digits. And a template claim whose
// span exists in another template is still bound to the wrong template.
func TestALongDigitRunIsBlockedEvenFromTheFactsAndATemplateIDMustBeTheRenderedOne(t *testing.T) {
	tpl := load(t)
	f := facts("o", "c")
	f.Facts[0].ValueMasked = "0123456789"
	r := explanation.Rendered{Text: "It is 0123456789.", Claims: []explanation.Claim{{Span: "It is 0123456789.", FactIDs: []string{"f1"}}}}
	p := explanation.Validate(tpl, r, f, nil)
	if len(p) != 1 || !strings.Contains(p[0], "check 4") {
		t.Fatalf("want exactly check 4, got %v", p)
	}
	sentence := "We do not have enough confirmed information to say what happened, so we cannot confirm it."
	a := "fin.outcome.exception.status_explained/INSUFFICIENT_EVIDENCE"
	b := "fin.outcome.exception.case_opened/INSUFFICIENT_EVIDENCE"
	f.OutcomeId, f.Classification = "fin.outcome.exception.status_explained", "INSUFFICIENT_EVIDENCE"
	rend := explanation.Rendered{Text: sentence, TemplateID: "en-NG/" + a, Claims: []explanation.Claim{{Span: sentence, TemplateID: "en-NG/" + b}}}
	if p := explanation.Validate(tpl, rend, f, nil); len(p) == 0 {
		t.Fatal("a claim bound to a template other than the rendered one passed")
	}
}

// Codex review of noema#224. A fact's own timestamp is fact data; a figure must match a whole figure of the facts (50 is not 50,000); a name the facts do not
// contain is blocked (the REQ-EXP-002 corpus has an INVENTED_NAME case).
func withTime(f contracts.ExplanationFactsV2) contracts.ExplanationFactsV2 {
	f.Facts[0].AsOf = "2026-11-05T10:42:00Z"
	f.Facts[0].SourceLabel = "Access Bank ledger"
	f.Facts[0].ValueMasked = "NGN 50,000"
	return f
}

func claimed(text string) explanation.Rendered {
	return explanation.Rendered{Text: text, Claims: []explanation.Claim{{Span: text, FactIDs: []string{"f1"}}}}
}

func TestACitedFactTimeIsAllowedAndAWholeFigureMustMatch(t *testing.T) {
	tpl := load(t)
	f := withTime(facts("o", "c"))
	for _, ok := range []string{"The payment of NGN 50,000 was read at 10:42.", "It was read at 10:42:00."} {
		if p := explanation.Validate(tpl, claimed(ok), f, nil); len(p) != 0 {
			t.Errorf("%q was blocked: %v", ok, p)
		}
	}
	for _, bad := range []string{"The payment of NGN 50 is confirmed.", "The payment of NGN 50000 is confirmed.", "The payment of NGN 5,000 is confirmed.", "It was read at 10:43.", "It was read at 11:42.", "It was read on 2026-11-06."} {
		if p := explanation.Validate(tpl, claimed(bad), f, nil); len(p) == 0 {
			t.Errorf("%q was not blocked", bad)
		}
	}
}

func TestAnInventedNameIsBlockedAndANameInTheFactsIsAllowed(t *testing.T) {
	tpl := load(t)
	f := withTime(facts("o", "c"))
	for _, bad := range []string{"Dr. Bello at the bank confirmed it.", "Bello confirmed it.", "We spoke to Adaeze about it.", "The payment went via Zenith."} {
		if p := explanation.Validate(tpl, claimed(bad), f, nil); len(p) == 0 {
			t.Errorf("%q was not blocked", bad)
		}
	}
	for _, ok := range []string{"Access Bank ledger shows the payment.", "We read it from the Access Bank ledger."} {
		if p := explanation.Validate(tpl, claimed(ok), f, nil); len(p) != 0 {
			t.Errorf("%q was blocked: %v", ok, p)
		}
	}
}

// Codex review of noema#224 (second round). A date is one value, not three numbers; a template claim must be the template the input facts select.
func TestADateIsComparedAsAWholeValue(t *testing.T) {
	tpl := load(t)
	f := withTime(facts("o", "c"))
	if p := explanation.Validate(tpl, claimed("It was read on 2026-11-05."), f, nil); len(p) != 0 {
		t.Errorf("the fact's own date was blocked: %v", p)
	}
	for _, bad := range []string{"It was read on 2026-05-11.", "It was read on 2026-11-06.", "It was read on 2025-11-05."} {
		if p := explanation.Validate(tpl, claimed(bad), f, nil); len(p) == 0 {
			t.Errorf("%q was not blocked", bad)
		}
	}
}

func TestATemplateForADifferentOutcomeOrClassificationThanTheFactsIsBlocked(t *testing.T) {
	tpl := load(t)
	success := facts("fin.outcome.exception.status_explained", "SUCCESS_CONFIRMED")
	r, err := explanation.Fallback(tpl, success)
	if err != nil {
		t.Fatal(err)
	}
	if p := explanation.Validate(tpl, r, success, nil); len(p) != 0 {
		t.Fatalf("the matching template was blocked: %v", p)
	}
	for name, other := range map[string]contracts.ExplanationFactsV2{
		"another classification": facts("fin.outcome.exception.status_explained", "INSUFFICIENT_EVIDENCE"),
		"another outcome":        facts("fin.outcome.exception.case_opened", "SUCCESS_CONFIRMED"),
	} {
		if p := explanation.Validate(tpl, r, other, nil); len(p) == 0 {
			t.Errorf("a SUCCESS_CONFIRMED template passed against %s facts", name)
		}
	}
	wrongLocale := success
	wrongLocale.Locale = contracts.ExplanationFactsV2LocalePcm
	if p := explanation.Validate(tpl, r, wrongLocale, nil); len(p) == 0 {
		t.Error("a template of another locale passed")
	}
}
