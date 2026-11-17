package answer_test

// EXP-01 / noema#611, ruling A237 (interim, relayed by the Founder, §3 of the Architect's message not committed): the core, not the UI, sets `done.verified` and `case_ref_masked`.
// `done.verified` is true only when there is at least one verdict and every verdict is ACHIEVED. `case_ref_masked` is present only after the case_opened outcome has an ACHIEVED
// verdict. The UI renders what is supplied and derives nothing (REQ-UI-001; PERD 13 rule 6, 40.1). The case-opened outcome ID comes from the pack, never from a literal here (INV-14).
// REQ-EXP-001, REQ-EXP-002, REQ-SEC-003; INV-13, AGENT-13. All data is SYNTHETIC.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/explanation/answer"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

const (
	caseOpened = "fin.outcome.exception.case_opened" // the pack's id, passed in
	statusOnly = "fin.outcome.exception.status_explained"
	maskedRef  = "CMP-…0001"
)

var (
	achieved = contracts.EnumsV1VerdictACHIEVED
	others   = []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictNOTACHIEVED, contracts.EnumsV1VerdictPARTIALLYACHIEVED, contracts.EnumsV1VerdictUNKNOWN, contracts.EnumsV1VerdictCOMPENSATED, contracts.EnumsV1VerdictPENDINGEXTERNAL}
)

// tx makes a customer sentence the only way the package allows: through the template renderer (explanation.Fallback) and answer.FromRendered (A3, relayed). The text is the
// pack template's text, so a test can put any sentence in it, including one that breaks the contract (Build must refuse it).
func tx(s string) answer.Text {
	dir, err := os.MkdirTemp("", "answer-templates")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	b, _ := json.Marshal(map[string]string{"fin.outcome.test/CLASS": s})
	if err := os.WriteFile(filepath.Join(dir, "en-NG.json"), b, 0o600); err != nil {
		panic(err)
	}
	tpl, err := explanation.LoadTemplates(dir, []string{"en-NG"})
	if err != nil {
		panic(err)
	}
	r, err := explanation.Fallback(tpl, contracts.ExplanationFactsV2{Locale: contracts.ExplanationFactsV2LocaleEnNG, OutcomeId: "fin.outcome.test", Classification: "CLASS"})
	if err != nil {
		panic(err)
	}
	t, err := answer.FromRendered(r)
	if err != nil {
		panic(err)
	}
	return t
}

func txs(ss ...string) []answer.Text {
	var out []answer.Text
	for _, s := range ss {
		out = append(out, tx(s))
	}
	return out
}

func base() answer.Input {
	return answer.Input{
		Found:               txs("Your transfer was debited from your account."),
		NotKnown:            []answer.NotKnown{{What: tx("Whether the recipient was credited"), Why: tx("The destination has not answered yet.")}},
		Summary:             tx("We opened a case for you."),
		Do:                  txs("Keep this message."),
		DoNot:               txs("Do not send the money again yet."),
		Next:                tx("We will update you here."),
		CaseOpenedOutcomeID: caseOpened,
	}
}

func v(outcome string, r contracts.EnumsV1Verdict) answer.Verdict {
	return answer.Verdict{OutcomeID: outcome, Result: r}
}

// A237: verified needs at least one verdict and every verdict ACHIEVED.
func TestVerifiedNeedsAtLeastOneVerdictAndEveryVerdictAchieved(t *testing.T) {
	if answer.Verified(nil) || answer.Verified([]answer.Verdict{}) {
		t.Error("no verdict is verified")
	}
	if !answer.Verified([]answer.Verdict{v(caseOpened, achieved)}) || !answer.Verified([]answer.Verdict{v(caseOpened, achieved), v(statusOnly, achieved)}) {
		t.Error("every verdict ACHIEVED is not verified")
	}
	for _, r := range others {
		if answer.Verified([]answer.Verdict{v(caseOpened, r)}) {
			t.Errorf("a %s verdict is verified", r)
		}
		if answer.Verified([]answer.Verdict{v(caseOpened, achieved), v(statusOnly, r)}) {
			t.Errorf("one ACHIEVED and one %s is verified", r)
		}
	}
	if answer.Verified([]answer.Verdict{v(caseOpened, "")}) || answer.Verified([]answer.Verdict{v(caseOpened, "achieved")}) {
		t.Error("an empty or lower-case verdict is verified: only the contract's ACHIEVED counts")
	}
}

// The built answer carries the core's decision in `done.verified`.
func TestTheBuiltAnswerCarriesTheVerifiedFlagTheCoreSets(t *testing.T) {
	in := base()
	in.Verdicts = []answer.Verdict{v(caseOpened, achieved)}
	a, err := answer.Build(in)
	if err != nil || !a.Done.Verified {
		t.Fatalf("answer %+v, err %v; want verified", a.Done, err)
	}
	in.Verdicts = []answer.Verdict{v(caseOpened, achieved), v(statusOnly, contracts.EnumsV1VerdictUNKNOWN)}
	if a, err = answer.Build(in); err != nil || a.Done.Verified {
		t.Fatalf("answer %+v, err %v; want not verified", a.Done, err)
	}
	in.Verdicts = nil
	if a, err = answer.Build(in); err != nil || a.Done.Verified {
		t.Fatalf("no verdict: %+v, err %v; want not verified", a.Done, err)
	}
}

// A237, PERD 40.1: the case reference only after case_opened is ACHIEVED. Anything else drops it (the answer is still built: the customer is told the truth without it).
func TestTheCaseReferenceIsPresentOnlyAfterCaseOpenedIsAchieved(t *testing.T) {
	in := base()
	in.CaseRefMasked = maskedRef
	cases := map[string][]answer.Verdict{
		"no verdict":                      nil,
		"another outcome ACHIEVED":        {v(statusOnly, achieved)},
		"case_opened UNKNOWN":             {v(caseOpened, contracts.EnumsV1VerdictUNKNOWN)},
		"case_opened NOT_ACHIEVED":        {v(caseOpened, contracts.EnumsV1VerdictNOTACHIEVED)},
		"case_opened PENDING_EXTERNAL":    {v(caseOpened, contracts.EnumsV1VerdictPENDINGEXTERNAL)},
		"case_opened ACHIEVED and FAILED": {v(caseOpened, achieved), v(caseOpened, contracts.EnumsV1VerdictNOTACHIEVED)},
		"case_opened PARTIALLY_ACHIEVED":  {v(caseOpened, contracts.EnumsV1VerdictPARTIALLYACHIEVED)},
		"case_opened COMPENSATED":         {v(caseOpened, contracts.EnumsV1VerdictCOMPENSATED)},
	}
	for name, vs := range cases {
		in.Verdicts = vs
		a, err := answer.Build(in)
		if err != nil || a.CaseRefMasked != nil {
			t.Errorf("%s: case ref %v, err %v; want none", name, a.CaseRefMasked, err)
		}
	}
	in.Verdicts = []answer.Verdict{v(caseOpened, achieved)}
	a, err := answer.Build(in)
	if err != nil || a.CaseRefMasked == nil || *a.CaseRefMasked != maskedRef {
		t.Fatalf("case_opened ACHIEVED: %v, err %v; want %q", a.CaseRefMasked, err, maskedRef)
	}
	// Fail closed: with no case-opened outcome configured, no reference is ever shown; and a reference that was not supplied is not made up.
	in.CaseOpenedOutcomeID = ""
	if a, err = answer.Build(in); err != nil || a.CaseRefMasked != nil {
		t.Errorf("no outcome configured: %v, %v", a.CaseRefMasked, err)
	}
	in.CaseOpenedOutcomeID, in.CaseRefMasked = caseOpened, ""
	if a, err = answer.Build(in); err != nil || a.CaseRefMasked != nil {
		t.Errorf("no reference supplied: %v, %v", a.CaseRefMasked, err)
	}
}

// The reference must be masked (a mask mark, at most four digits in all) or the answer is refused: a raw reference never leaves, even after an ACHIEVED verdict.
func TestAReferenceThatIsNotMaskedIsRefused(t *testing.T) {
	in := base()
	in.Verdicts = []answer.Verdict{v(caseOpened, achieved)}
	for _, bad := range []string{"CMP-2026-00001", "CMP-…00001", "CMP-…0001-0002", strings.Repeat("x…", 40), "no mark here"} {
		in.CaseRefMasked = bad
		if _, err := answer.Build(in); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// Every text member is held to the contract: no run of five digits (any script), the length bounds, and the list bounds. A refusal is an error, never a masked guess.
func TestATextThatBreaksTheContractIsRefused(t *testing.T) {
	bads := map[string]func(*answer.Input){
		"a digit run in found":    func(i *answer.Input) { i.Found = txs("Your account 123456789 was debited.") },
		"digits joined by spaces": func(i *answer.Input) { i.Summary = tx("We opened case 1234 5678 for you.") },
		"arabic-indic digits":     func(i *answer.Input) { i.Next = tx("Call ٠١٢٣٤٥٦٧٨٩ today.") },
		"an empty summary":        func(i *answer.Input) { i.Summary = answer.Text{} },
		"a summary over 300":      func(i *answer.Input) { i.Summary = tx(strings.Repeat("a", 301)) },
		"nine found items": func(i *answer.Input) {
			i.Found = make([]answer.Text, 9)
			for k := range i.Found {
				i.Found[k] = tx("x")
			}
		},
		"an empty found item":             func(i *answer.Input) { i.Found = []answer.Text{{}} },
		"a not-known item with no reason": func(i *answer.Input) { i.NotKnown = []answer.NotKnown{{What: tx("x")}} },
		"an empty next":                   func(i *answer.Input) { i.Next = answer.Text{} },
	}
	for name, mut := range bads {
		in := base()
		mut(&in)
		if _, err := answer.Build(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := answer.Build(base()); err != nil {
		t.Fatalf("the good input: %v", err)
	}
}

// A verdict with no outcome ID must never count as the case-opened outcome when none is configured (an empty ID equals an empty ID).
func TestAnEmptyOutcomeIDIsNeverTheCaseOpenedOutcome(t *testing.T) {
	in := base()
	in.CaseOpenedOutcomeID, in.CaseRefMasked = "", maskedRef
	in.Verdicts = []answer.Verdict{v("", achieved)}
	if a, err := answer.Build(in); err != nil || a.CaseRefMasked != nil {
		t.Errorf("case ref %v, err %v; want none", a.CaseRefMasked, err)
	}
}

// "At most four digits in all": digits that are not next to each other still count, so a reference with five separated digits is refused although no run is long.
func TestAMaskedReferenceShowsAtMostFourDigitsInAll(t *testing.T) {
	in := base()
	in.Verdicts = []answer.Verdict{v(caseOpened, achieved)}
	in.CaseRefMasked = "CMP-1…A2B3C4D5"
	if _, err := answer.Build(in); err == nil {
		t.Error("five digits in all were accepted")
	}
	in.CaseRefMasked = "CMP-1…A2B3C4D"
	if _, err := answer.Build(in); err != nil {
		t.Errorf("four digits in all were refused: %v", err)
	}
}

// The wire form: schema_version, the six parts, `case_ref_masked` only when present, empty lists as lists, and no member the contract does not have.
func TestTheWireFormHasTheSixPartsAndNothingElse(t *testing.T) {
	in := base()
	in.Verdicts = []answer.Verdict{v(caseOpened, achieved)}
	in.CaseRefMasked = maskedRef
	a, err := answer.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema_version", "found", "not_known", "done", "advice", "next", "case_ref_masked"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s: %s", k, raw)
		}
	}
	if len(m) != 7 {
		t.Errorf("members %v; want exactly the contract's seven", m)
	}
	in.Verdicts = nil
	a, _ = answer.Build(in)
	raw, _ = json.Marshal(a)
	if strings.Contains(string(raw), "case_ref_masked") {
		t.Errorf("a case reference without its verdict is on the wire: %s", raw)
	}
	var round contracts.CustomerAnswerV1
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Errorf("the contract's own parser refuses the built answer: %v", err)
	}
}

// SR on #618, note 1: an outcome that is not ACHIEVED must say so. A verdict that is not ACHIEVED with no not_known item is refused, whatever the summary says (an UNKNOWN outcome
// is never told to the customer as done); with an item it builds, and `verified` stays false.
func TestAVerdictThatIsNotAchievedNeedsANotKnownItem(t *testing.T) {
	for _, r := range others {
		in := base()
		in.NotKnown = nil
		in.Verdicts = []answer.Verdict{v(caseOpened, r)}
		if _, err := answer.Build(in); err == nil {
			t.Errorf("%s with no not_known item was accepted", r)
		}
		in.Verdicts = []answer.Verdict{v(statusOnly, achieved), v(caseOpened, r)}
		if _, err := answer.Build(in); err == nil {
			t.Errorf("ACHIEVED next to %s with no not_known item was accepted", r)
		}
		in.NotKnown = base().NotKnown
		a, err := answer.Build(in)
		if err != nil || a.Done.Verified {
			t.Errorf("%s with an item: verified %v, err %v; want built and not verified", r, a.Done.Verified, err)
		}
	}
	in := base()
	in.NotKnown = nil
	in.Verdicts = []answer.Verdict{v(caseOpened, achieved)}
	if a, err := answer.Build(in); err != nil || !a.Done.Verified {
		t.Errorf("all ACHIEVED needs no not_known item: %v, %v", a.Done.Verified, err)
	}
}

// SR on #618, note 3: the contract's own invalid case references (customer-answer.v1 and investigation-response.v1 fixtures) are refused by Build, even after an ACHIEVED verdict.
func TestTheContractsInvalidCaseReferencesAreRefused(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "contracts", "fixtures", "invalid")
	files := []string{"customer-answer/case_ref_unmasked.json", "investigation-response/case_ref_unmasked.json", "investigation-response/case_ref_without_a_mask.json", "investigation-response/case_ref_year_and_last_four.json"}
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		var fx struct {
			Ref string `json:"case_ref_masked"`
		}
		if err := json.Unmarshal(raw, &fx); err != nil || fx.Ref == "" {
			t.Fatalf("%s: no case_ref_masked (%v)", f, err)
		}
		in := base()
		in.Verdicts = []answer.Verdict{v(caseOpened, achieved)}
		in.CaseRefMasked = fx.Ref
		if _, err := answer.Build(in); err == nil {
			t.Errorf("%s: %q was accepted", f, fx.Ref)
		}
	}
}

// FE contract review on #618: the bounds of customer-answer.v1 that the generated TypeScript parser enforces. `next.when` is at most 100 characters and each of `advice.do` and
// `advice.do_not` at most 4 items (the other text members 300, `found` and `not_known` 8). The bounds are read from the contract file, so a change there changes this test.
func TestTheBoundsAreTheContractsAndBuildRefusesOneBeyondThem(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "schemas", "customer-answer.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Props struct {
			Found  struct{ MaxItems int } `json:"found"`
			Advice struct {
				Props struct {
					Do    struct{ MaxItems int } `json:"do"`
					DoNot struct{ MaxItems int } `json:"do_not"`
				} `json:"properties"`
			} `json:"advice"`
			Next struct {
				Props struct {
					When struct{ MaxLength int } `json:"when"`
				} `json:"properties"`
			} `json:"next"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	// the raw tags above use Go's case-insensitive match on "maxItems"/"maxLength"
	if c.Props.Advice.Props.Do.MaxItems != 4 || c.Props.Advice.Props.DoNot.MaxItems != 4 || c.Props.Next.Props.When.MaxLength != 100 || c.Props.Found.MaxItems != 8 {
		t.Fatalf("the contract's bounds changed: do %d, do_not %d, when %d, found %d; update answer.go and this test", c.Props.Advice.Props.Do.MaxItems, c.Props.Advice.Props.DoNot.MaxItems, c.Props.Next.Props.When.MaxLength, c.Props.Found.MaxItems)
	}
	in := base()
	in.Verdicts = []answer.Verdict{v(caseOpened, achieved)}
	at := func(mut func(*answer.Input), wantOK bool, what string) {
		t.Helper()
		x := in
		mut(&x)
		if _, err := answer.Build(x); (err == nil) != wantOK {
			t.Errorf("%s: err %v, want ok=%v", what, err, wantOK)
		}
	}
	when := func(n int) func(*answer.Input) {
		return func(i *answer.Input) { w := tx(strings.Repeat("w", n)); i.When = &w }
	}
	at(when(100), true, "next.when at 100")
	at(when(101), false, "next.when at 101")
	at(func(i *answer.Input) { i.Do = txs("a", "b", "c", "d") }, true, "advice.do at 4")
	at(func(i *answer.Input) { i.Do = txs("a", "b", "c", "d", "e") }, false, "advice.do at 5")
	at(func(i *answer.Input) { i.DoNot = txs("a", "b", "c", "d") }, true, "advice.do_not at 4")
	at(func(i *answer.Input) { i.DoNot = txs("a", "b", "c", "d", "e") }, false, "advice.do_not at 5")
	at(func(i *answer.Input) { i.Found = txs("a", "b", "c", "d", "e", "f", "g", "h") }, true, "found at 8")
	at(func(i *answer.Input) { i.Summary = tx(strings.Repeat("s", 300)) }, true, "summary at 300")
	at(func(i *answer.Input) { i.Summary = tx(strings.Repeat("s", 301)) }, false, "summary at 301")
	at(func(i *answer.Input) { i.Next = tx(strings.Repeat("n", 300)) }, true, "next.what at 300")
	at(func(i *answer.Input) { i.Next = tx(strings.Repeat("n", 301)) }, false, "next.what at 301")
}

// The length is counted in characters (code points), as the contract's maxLength is: 300 two-byte letters is allowed, and 301 is not.
func TestLengthIsCountedInCharactersNotBytes(t *testing.T) {
	in := base()
	in.Summary = tx(strings.Repeat("é", 300))
	if _, err := answer.Build(in); err != nil {
		t.Errorf("300 characters of two bytes each: %v", err)
	}
	in.Summary = tx(strings.Repeat("é", 301))
	if _, err := answer.Build(in); err == nil {
		t.Error("301 characters were accepted")
	}
}

// A3 (Architect, interim, relayed): Build accepts only the renderer's type. The text members of Input are `answer.Text`, which only FromRendered makes, from a Rendered that
// the renderer itself produced (explanation.Fallback); no text member of Input is a plain string, so no caller can hand Build a model's sentence or a literal.
func TestNoTextMemberOfInputIsAPlainString(t *testing.T) {
	allowed := map[string]bool{"CaseOpenedOutcomeID": true, "CaseRefMasked": true} // the pack's outcome ID and the already-masked reference, not customer sentences
	ty := reflect.TypeOf(answer.Input{})
	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		if allowed[f.Name] || f.Name == "Verdicts" {
			continue
		}
		switch f.Type.String() {
		case "string", "[]string", "*string":
			t.Errorf("Input.%s is %s: a customer sentence must be an answer.Text", f.Name, f.Type)
		}
	}
	nk := reflect.TypeOf(answer.NotKnown{})
	for i := 0; i < nk.NumField(); i++ {
		if nk.Field(i).Type.Kind() == reflect.String {
			t.Errorf("NotKnown.%s is a plain string", nk.Field(i).Name)
		}
	}
}

// FromRendered refuses what the renderer did not make: a Rendered built by hand (even with every field set), an empty one, and the zero Text is refused by Build.
func TestFromRenderedRefusesAHandBuiltRendered(t *testing.T) {
	forged := explanation.Rendered{Text: "We opened a case for you.", TemplateID: "en-NG/fin.outcome.x/CLASS", TemplateVersion: "1", Claims: []explanation.Claim{{Span: "We opened a case for you.", TemplateID: "en-NG/fin.outcome.x/CLASS"}}}
	if _, err := answer.FromRendered(forged); err == nil {
		t.Error("a hand-built Rendered was accepted")
	}
	if _, err := answer.FromRendered(explanation.Rendered{}); err == nil {
		t.Error("an empty Rendered was accepted")
	}
	in := base()
	in.Summary = answer.Text{}
	if _, err := answer.Build(in); err == nil {
		t.Error("the zero Text was accepted as a summary")
	}
}
