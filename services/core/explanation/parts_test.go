package explanation_test

// EXP-01 / A271 (Architect, interim, relayed, not yet committed): the answer has six parts and a pack template key per part. The summary keeps the key `<outcome_id>/<classification>`;
// every other part is `<outcome_id>/<classification>/<part>` with a part of a closed list. A part with no template is blocked, never borrowed from the summary or another part (the
// same rule as a missing summary, ErrNoTemplate); a part's text is a template-bound claim like the summary and goes through the same Validate. The wording of the sentences is the
// Founder's and a native speaker's (REQ-EXP-002): this file proves the mechanics with SYNTHETIC sentences, not the pack's. REQ-EXP-002, INV-13, AGENT-17; PERD 31.2.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/explanation"
)

const (
	partOutcome = "fin.outcome.exception.status_explained"
	partClass   = "SUCCESS_CONFIRMED"
)

// partTemplates writes a one-locale template set in a temp directory: the summary and the given parts, as SYNTHETIC sentences.
func partTemplates(t *testing.T, doc map[string]string) explanation.Templates {
	t.Helper()
	dir := t.TempDir()
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(dir, "en-NG.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	tpl, err := explanation.LoadTemplates(dir, []string{"en-NG"})
	if err != nil {
		t.Fatal(err)
	}
	return tpl.WithMaskingLimits(packMaskingLimits(t))
}

func TestEveryPartOfTheAnswerHasAKey(t *testing.T) {
	want := []explanation.Part{explanation.PartFound, explanation.PartNotKnownWhat, explanation.PartNotKnownWhy, explanation.PartAdviceDo, explanation.PartAdviceDoNot, explanation.PartNextWhat, explanation.PartNextWhen}
	got := explanation.Parts()
	if len(got) != len(want) {
		t.Fatalf("parts %v, want %v", got, want)
	}
	seen := map[explanation.Part]bool{}
	for _, p := range got {
		if p == "" || seen[p] {
			t.Errorf("part %q is empty or listed twice", p)
		}
		seen[p] = true
	}
	for _, p := range want {
		if !seen[p] {
			t.Errorf("part %q is not listed", p)
		}
	}
}

// A part is found by its own key and nothing else: the rendered text, the template ID (with the part) and the claim bind to the part's template.
func TestAPartIsRenderedFromItsOwnKey(t *testing.T) {
	key := partOutcome + "/" + partClass
	tpl := partTemplates(t, map[string]string{key: "This transaction is confirmed as successful.", key + "/found": "Your account was debited.", key + "/advice_do_not": "Do not send the money again yet."})
	f := facts(partOutcome, partClass)
	sum, err := explanation.Fallback(tpl, f)
	if err != nil || sum.Text != "This transaction is confirmed as successful." || sum.TemplateID != "en-NG/"+key {
		t.Fatalf("summary: %+v, %v", sum, err)
	}
	for part, want := range map[explanation.Part]string{explanation.PartFound: "Your account was debited.", explanation.PartAdviceDoNot: "Do not send the money again yet."} {
		r, err := explanation.FallbackPart(tpl, f, part)
		if err != nil || r.Text != want || r.TemplateID != "en-NG/"+key+"/"+string(part) || len(r.Claims) != 1 || r.Claims[0].TemplateID != r.TemplateID {
			t.Errorf("%s: %+v, %v", part, r, err)
		}
		if p := explanation.Validate(tpl, r, f, nil); len(p) != 0 {
			t.Errorf("%s: a template-bound part fails validation: %v", part, p)
		}
	}
}

// A missing part is blocked with ErrNoTemplate: it is never the summary, never another part, never another class's sentence.
func TestAMissingPartIsBlockedNeverBorrowed(t *testing.T) {
	key := partOutcome + "/" + partClass
	tpl := partTemplates(t, map[string]string{key: "This transaction is confirmed as successful.", key + "/found": "Your account was debited."})
	f := facts(partOutcome, partClass)
	for _, p := range []explanation.Part{explanation.PartAdviceDo, explanation.PartNextWhat, explanation.PartNotKnownWhy} {
		if r, err := explanation.FallbackPart(tpl, f, p); !errors.Is(err, explanation.ErrNoTemplate) || r.Text != "" {
			t.Errorf("%s: %+v, %v; want ErrNoTemplate and no text", p, r, err)
		}
	}
	// a part outside the closed list is refused whatever the pack holds, so a key the contract does not know is never read
	odd := partTemplates(t, map[string]string{key: "This transaction is confirmed as successful.", key + "/summary": "x", key + "/nope": "x"})
	for _, p := range []explanation.Part{"", "summary", "nope", "FOUND", "found "} {
		if r, err := explanation.FallbackPart(odd, f, p); err == nil || r.Text != "" {
			t.Errorf("part %q: %+v, %v; want a refusal", p, r, err)
		}
	}
}

// The summary lookup never returns a part: Fallback reads only the two-segment key, so a part's key cannot stand in for a missing summary.
func TestAPartKeyNeverStandsInForTheSummary(t *testing.T) {
	key := partOutcome + "/" + partClass
	tpl := partTemplates(t, map[string]string{key + "/found": "Your account was debited."})
	if r, err := explanation.Fallback(tpl, facts(partOutcome, partClass)); !errors.Is(err, explanation.ErrNoTemplate) || r.Text != "" {
		t.Errorf("%+v, %v", r, err)
	}
}

// A part may hold a fact slot, filled by the same rule as the summary (exactly one KNOWN fact of the kind, else ErrSlot).
func TestAPartFillsItsSlotsByTheSameRule(t *testing.T) {
	key := partOutcome + "/" + partClass
	tpl := partTemplates(t, map[string]string{key: "This transaction is confirmed as successful.", key + "/found": "Your account {fact:account} was debited."})
	f := facts(partOutcome, partClass)
	r, err := explanation.FallbackPart(tpl, f, explanation.PartFound)
	if err != nil || r.Text != "Your account ending 0001 was debited." || len(r.FactIDs) != 1 || r.FactIDs[0] != "f2" {
		t.Fatalf("%+v, %v", r, err)
	}
	f.Facts = f.Facts[:1] // no account fact: blocked
	if r, err := explanation.FallbackPart(tpl, f, explanation.PartFound); !errors.Is(err, explanation.ErrSlot) || r.Text != "" {
		t.Errorf("%+v, %v", r, err)
	}
}

// Validate accepts a template of this outcome and class and one of its parts, and nothing else: another part name, another class's part or a part of another outcome is refused.
func TestValidateRefusesATemplateThatIsNotThisClassOrAPartOfIt(t *testing.T) {
	key := partOutcome + "/" + partClass
	tpl := partTemplates(t, map[string]string{key: "This transaction is confirmed as successful.", key + "/found": "Your account was debited.",
		partOutcome + "/REVERSED_CONFIRMED/found": "Your account was debited.", "fin.outcome.exception.case_opened/" + partClass + "/found": "Your account was debited.",
		key + "/nope": "Your account was debited.", key + "/found/found": "Your account was debited."}) // loaded on purpose: only the part rule can refuse them
	f := facts(partOutcome, partClass)
	r, err := explanation.FallbackPart(tpl, f, explanation.PartFound)
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{
		"another class":   "en-NG/" + partOutcome + "/REVERSED_CONFIRMED/found",
		"another outcome": "en-NG/fin.outcome.exception.case_opened/" + partClass + "/found",
		"an unknown part": "en-NG/" + key + "/nope",
		"a deeper path":   "en-NG/" + key + "/found/found",
	} {
		x := r
		x.TemplateID = id
		x.Claims = []explanation.Claim{{Span: x.Text, TemplateID: id}}
		if p := explanation.Validate(tpl, x, f, nil); len(p) == 0 {
			t.Errorf("%s: %q was accepted", name, id)
		}
	}
}

// The pack checker (tools/packcheck) and the loader share one closed list of parts: a part added to one and not the other fails here.
func TestThePackCheckersPartsAreTheLoadersParts(t *testing.T) {
	raw, err := os.ReadFile("../../../tools/packcheck/checks.py")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`TEMPLATE_PARTS\s*=\s*\(([^)]*)\)`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("tools/packcheck/checks.py has no TEMPLATE_PARTS")
	}
	var py []string
	for _, s := range regexp.MustCompile(`"([a-z_]+)"`).FindAllStringSubmatch(m[1], -1) {
		py = append(py, s[1])
	}
	var gop []string
	for _, p := range explanation.Parts() {
		gop = append(gop, string(p))
	}
	if strings.Join(py, ",") != strings.Join(gop, ",") {
		t.Errorf("packcheck parts %v, loader parts %v", py, gop)
	}
}
