package explanation_test

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/explanation"
)

// A757 (AMB-04; REQ-AMB-004; AGENT-09): core turns a pack's question key into the text of one locale, server-side; the front end only displays it. The text is the pack's, fixed, with no
// placeholder filled from the customer's words. A key or a locale the pack does not have is an error, never another locale's text and never the key itself.
func TestAQuestionKeyIsTurnedIntoTheTextOfItsLocale(t *testing.T) {
	tpl, err := explanation.LoadTemplates("../../../packs/fintech/templates", []string{"en-NG"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := tpl.Question("en-NG", "fin.clarify.when")
	if err != nil || got != "On which day, or at what time, did this happen?" {
		t.Fatalf("Question = %q, %v; want the pack's en-NG text", got, err)
	}
}

func TestAnUnknownKeyOrLocaleIsAnErrorAndNeverAFallback(t *testing.T) {
	tpl, err := explanation.LoadTemplates("../../../packs/fintech/templates", []string{"en-NG"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{{"en-NG", "fin.clarify.nothing"}, {"yo", "fin.clarify.when"}, {"en-NG", ""}, {"", "fin.clarify.when"}} {
		if got, err := tpl.Question(c[0], c[1]); !errors.Is(err, explanation.ErrNoQuestion) || got != "" {
			t.Errorf("Question(%q, %q) = %q, %v; want ErrNoQuestion and no text", c[0], c[1], got, err)
		}
	}
}

// A key that names an outcome template (`<outcome>/<classification>`) is not a question key: explanation templates are not questions.
func TestAnOutcomeTemplateIsNotAQuestion(t *testing.T) {
	tpl, err := explanation.LoadTemplates("../../../packs/fintech/templates", []string{"en-NG"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tpl.Question("en-NG", "fin.outcome.exception.case_opened/CONTRADICTORY_STATE"); !errors.Is(err, explanation.ErrNoQuestion) {
		t.Errorf("an outcome template was served as a question: %v", err)
	}
}
