package explanation_test

// SEC-03 / PERD 31.2 check 4 (line 3273 of PERD v1.22: no unmasked digit run longer than 4), REQ-EXP-002, AGENT-16: the validator reads the longest unmasked run from
// the pack's masking-limits.v1 document, the same one the redactors and the error scanner read. Ruling A154 as corrected (relayed, not yet committed). SYNTHETIC.

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/platform/masking"
)

func limit(t *testing.T, v string) masking.Limits {
	t.Helper()
	l, err := masking.Load(strings.NewReader(`{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","max_unmasked_digit_run":` + v + `}`))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func check4(t *testing.T, tpl explanation.Templates, text string) []string {
	t.Helper()
	var out []string
	r := explanation.Rendered{Text: text}
	for _, p := range explanation.Validate(tpl, r, facts("o", "c"), nil) {
		if strings.HasPrefix(p, "check 4") {
			out = append(out, p)
		}
	}
	return out
}

// With the pack's limit (4) a run of five digits is blocked and a run of four is not; a run of any other script counts the same.
func TestThePacksLimitBlocksTheFirstRunLongerThanItsValue(t *testing.T) {
	tpl := load(t)
	if got := check4(t, tpl, "Reference 1234."); len(got) != 0 {
		t.Errorf("a run of 4 is allowed: %v", got)
	}
	for name, text := range map[string]string{"5 digits": "Reference 12345.", "full-width": "Reference １２３４５.", "Arabic-Indic": "Reference ١٢٣٤٥."} {
		if len(check4(t, tpl, text)) == 0 {
			t.Errorf("%s: a run of 5 was not blocked", name)
		}
	}
}

// The value is read, not remembered: with a limit of 6 the same five-digit run passes and a seven-digit run is blocked.
func TestTheValidatorReadsTheLimitItWasGiven(t *testing.T) {
	tpl := load(t).WithMaskingLimits(limit(t, "6"))
	if got := check4(t, tpl, "Reference 12345 and 123456."); len(got) != 0 {
		t.Errorf("runs of 5 and 6 are allowed under a limit of 6: %v", got)
	}
	if len(check4(t, tpl, "Reference 1234567.")) == 0 {
		t.Error("a run of 7 passed under a limit of 6")
	}
}

// Fail closed: templates that were never given the limit block every text, with a reason that says why, instead of using a number of their own.
func TestTemplatesWithoutTheLimitBlockEveryText(t *testing.T) {
	tpl, err := explanation.LoadTemplates(packTemplates, locales)
	if err != nil {
		t.Fatal(err)
	}
	got := check4(t, tpl, "Your payment is confirmed.")
	if len(got) != 1 || !strings.Contains(got[0], "limit is not loaded") {
		t.Fatalf("got %v, want one problem saying the limit is not loaded", got)
	}
}
