package explanation_test

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/explanation"
)

// EL's reviewer-role statement on noema#224 (blocking finding): the figure, digit-run and word patterns were ASCII-only, so an unmasked number in fullwidth or
// Arabic-Indic digits, or an invented name in another script, passed PERD 31.2 checks 2 and 4 (AGENT-13). A KNOWN fact must not let them through. SYNTHETIC.
func TestFiguresAndNamesInAnyScriptAreCheckedLikeTheirASCIIForms(t *testing.T) {
	tpl := load(t)
	f := facts("fin.outcome.exception.status_explained", "SUCCESS_CONFIRMED")
	cases := map[string]string{
		"fullwidth digits":              "Your payment of NGN ０１２３４５６７８９ is confirmed.",
		"Arabic-Indic digits":           "Your payment of NGN ٠١٢٣٤٥٦٧٨٩ is confirmed.",
		"Devanagari digits":             "Your payment of NGN ०१२३४५६७८९ is confirmed.",
		"a short fullwidth figure":      "Your payment of NGN １２ is confirmed.",
		"a Cyrillic name":               "Иван is confirmed.",
		"a Greek name":                  "Νίκος is confirmed.",
		"a Latin name with accents":     "José is confirmed.",
		"a name with a combining mark":  "José is confirmed.",
		"a Hausa-looking invented name": "Ɗanjuma is confirmed.",
	}
	for name, text := range cases {
		r := explanation.Rendered{Text: text, Claims: []explanation.Claim{{Span: text, FactIDs: []string{"f1"}}}}
		problems := explanation.Validate(tpl, r, f, nil)
		if len(problems) == 0 {
			t.Errorf("%s passed the validator: %q", name, text)
		}
	}
	// Long unmasked digit runs in other scripts are check 4 specifically.
	for _, text := range []string{"Account ０１２３４５６７８９ is confirmed.", "Account ٠١٢٣٤٥٦٧٨٩ is confirmed."} {
		r := explanation.Rendered{Text: text, Claims: []explanation.Claim{{Span: text, FactIDs: []string{"f1"}}}}
		found := false
		for _, p := range explanation.Validate(tpl, r, f, nil) {
			found = found || strings.HasPrefix(p, "check 4")
		}
		if !found {
			t.Errorf("%q was not blocked by check 4", text)
		}
	}
}

// The pack's own fallbacks are still accepted: the wider patterns must not block the safe path.
func TestEveryPackTemplateStillPassesTheWiderPatterns(t *testing.T) {
	TestEveryPackTemplateRendersAndPassesTheValidator(t)
}
