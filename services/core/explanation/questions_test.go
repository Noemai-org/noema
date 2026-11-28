package explanation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/Noemai-org/noema/services/core/explanation"
)

// summaryKeys are the fallback summaries of a locale, keyed <outcome_id>/<classification>. A key with no slash is the question a slot asks (A757), not a summary: the summary tests
// run over the first, the question tests below over the second.
func summaryKeys(tpl explanation.Templates) []string {
	var keys []string
	for _, k := range tpl.Keys("en-NG") {
		if strings.Contains(k, "/") {
			keys = append(keys, k)
		}
	}
	return keys
}

func questionKeys(tpl explanation.Templates) []string {
	var keys []string
	for _, k := range tpl.Keys("en-NG") {
		if !strings.Contains(k, "/") {
			keys = append(keys, k)
		}
	}
	return keys
}

// packSlotQuestionKeys reads the question keys the real pack's slots declare. SYNTHETIC test input is not needed: the pack is data.
func packSlotQuestionKeys(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(packTemplates, "..", "..", "fintech_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pack struct {
		IntentNodes []struct {
			Slots []struct {
				QuestionKey string `json:"question_key"`
			} `json:"slots"`
		} `json:"intent_nodes"`
	}
	if err := json.Unmarshal(raw, &pack); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, n := range pack.IntentNodes {
		for _, s := range n.Slots {
			if s.QuestionKey != "" {
				keys = append(keys, s.QuestionKey)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// A757 (interim; AMB-04; A563, A582): the en-NG file holds a text for each SLOT question the pack's slots name, and only those. A question is plain language: one question, no digit,
// no placeholder, nothing the forbidden-claim list blocks. The loader (which accepts any non-empty text under a key) loads it.
func TestEveryQuestionTheSlotsNameHasAPlainEnglishTextAndNoOtherKeyHasNoSlash(t *testing.T) {
	tpl := load(t)
	want := packSlotQuestionKeys(t)
	if len(want) == 0 {
		t.Fatal("the pack names no slot question")
	}
	got := questionKeys(tpl)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("en-NG keys with no slash %v, want the pack's slot question keys %v", got, want)
	}
	raw, err := os.ReadFile(filepath.Join(packTemplates, "en-NG.json"))
	if err != nil {
		t.Fatal(err)
	}
	var texts map[string]string
	if err := json.Unmarshal(raw, &texts); err != nil {
		t.Fatal(err)
	}
	patterns, err := explanation.LoadForbidden(packTemplates, "en-NG")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range got {
		text := texts[key]
		if strings.Count(text, "?") != 1 || !strings.HasSuffix(strings.TrimSpace(text), "?") {
			t.Errorf("%s: a question is one sentence ending with one question mark: %q", key, text)
		}
		if strings.ContainsAny(text, "{}") || strings.IndexFunc(text, unicode.IsDigit) >= 0 {
			t.Errorf("%s: a question holds no placeholder and no digit: %q", key, text)
		}
		for _, p := range patterns {
			if regexp.MustCompile(p).MatchString(text) {
				t.Errorf("%s: the forbidden-claim pattern %s blocks the question %q", key, p, text)
			}
		}
	}
}
