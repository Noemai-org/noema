package explanation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/explanation"
)

// Architect decision A3 (8 Nov 2026), REQ-EXP-002, PERD 31.2 check 3: the forbidden-claim pattern lists are pack data, one file per locale next to
// the templates, `<locale>.forbidden.json`, a JSON array of regular expressions. They are loaded as data and handed to Validate.

func writeForbidden(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "en-NG.forbidden.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestForbiddenPatternsLoadFromTheSiblingDataFile(t *testing.T) {
	patterns, err := explanation.LoadForbidden(packTemplates, "en-NG")
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) < 5 {
		t.Fatalf("%d patterns: the PERD 31.2 list has at least five claims", len(patterns))
	}
	// The templates loader reads only <locale>.json, so the sibling file is not mistaken for a locale.
	if _, err := explanation.LoadTemplates(packTemplates, locales); err != nil {
		t.Fatal(err)
	}
}

// A fallback the pack wrote itself must never be blocked by the pack's own list (that would make the safe path unreachable).
func TestEveryPackTemplatePassesItsOwnForbiddenList(t *testing.T) {
	tpl := load(t)
	patterns, err := explanation.LoadForbidden(packTemplates, "en-NG")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range tpl.Keys("en-NG") {
		outcome, class, _ := strings.Cut(key, "/")
		f := facts(outcome, class)
		r, err := explanation.Fallback(tpl, f)
		if err != nil {
			t.Fatal(err)
		}
		if p := explanation.Validate(tpl, r, f, patterns); len(p) != 0 {
			t.Errorf("%s: blocked by the pack's own lists: %v", key, p)
		}
	}
}

// The five claims of PERD 31.2: a text that makes one is blocked by check 3, whatever it is bound to.
func TestTheLoadedListBlocksTheClaimsPERDNames(t *testing.T) {
	tpl := load(t)
	patterns, err := explanation.LoadForbidden(packTemplates, "en-NG")
	if err != nil {
		t.Fatal(err)
	}
	f := facts("fin.outcome.exception.status_explained", "SUCCESS_CONFIRMED")
	for _, text := range []string{
		"Your money is safe.",
		"It will be reversed in 24 hours.",
		"The receiving bank failed.",
		"Your complaint is resolved.",
		"Your complaint is done.",
		"We fixed it.",
		"We guarantee the refund.",
	} {
		r := explanation.Rendered{Text: text, Claims: []explanation.Claim{{Span: text, FactIDs: []string{"f1"}}}}
		blocked := false
		for _, p := range explanation.Validate(tpl, r, f, patterns) {
			blocked = blocked || strings.HasPrefix(p, "check 3")
		}
		if !blocked {
			t.Errorf("%q was not blocked by check 3", text)
		}
	}
}

// A locale with no usable list cannot be validated, so it fails closed: no file, an empty list, a pattern that does not compile, an empty pattern,
// a duplicate, or a file that is not an array of strings.
func TestAMissingOrUnusableForbiddenFileFailsClosed(t *testing.T) {
	if _, err := explanation.LoadForbidden(t.TempDir(), "en-NG"); err == nil {
		t.Error("a locale with no forbidden file was accepted")
	}
	for name, content := range map[string]string{
		"an empty list":           `[]`,
		"an invalid pattern":      `["("]`,
		"an empty pattern":        `["(?i)ok", ""]`,
		"a blank pattern":         `["(?i)ok", "   "]`,
		"a duplicate":             `["(?i)ok", "(?i)ok"]`,
		"an object":               `{"a": "b"}`,
		"an array of non-strings": `[1, 2]`,
		"not JSON":                `nope`,
	} {
		if _, err := explanation.LoadForbidden(writeForbidden(t, content), "en-NG"); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if got, err := explanation.LoadForbidden(writeForbidden(t, `["(?i)\\bone\\b", "(?i)\\btwo\\b"]`), "en-NG"); err != nil || len(got) != 2 {
		t.Errorf("a valid file gave %v, %v", got, err)
	}
}
