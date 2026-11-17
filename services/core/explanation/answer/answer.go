// Package answer builds the customer's answer (customer-answer.v1) and is where the core, not the UI, decides what is verified (ruling A237, interim, relayed; noema#611; PERD 13
// rule 6 and 40.1; REQ-UI-001). `done.verified` is true only when there is at least one verdict and every verdict is ACHIEVED. `case_ref_masked` is present only when the case-opened
// outcome has an ACHIEVED verdict. The UI renders what it is given and derives nothing.
//
// May import: the standard library, internal/contracts, explanation (the renderer's output type only, A3), record/payloadguard (the one digit definition, A194).
//
// Must never: name an outcome or an industry term (the pack gives the case-opened outcome's ID, INV-14); show a reference that is not masked; guess, mask or trim a text that breaks
// the contract (it is an error); read the Record, a clock or a store; import decision, grant or verdict code (explanation output is never an input to them, SoT 51).
package answer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

// Bounds of the contract (customer-answer.v1): frame bounds, not policy.
const (
	maxText       = 300
	maxWhen       = 100 // next.when
	maxItems      = 8   // found, not_known
	maxAdvice     = 4   // advice.do and advice.do_not, each
	maxCaseRef    = 64
	schemaVersion = "1.0"
)

// Text is a customer sentence that came from the template renderer (A3, Architect, interim, relayed). The only way to make one is FromRendered, from a Rendered that
// explanation.Fallback produced; the zero Text is empty and Build refuses it. A caller therefore cannot hand Build a model's sentence or a literal.
type Text struct{ s string }

// FromRendered makes a Text from the renderer's own output. A Rendered built by hand, or with no text, is refused.
func FromRendered(r explanation.Rendered) (Text, error) {
	if !r.FromRenderer() || r.Text == "" || r.TemplateID == "" {
		return Text{}, fmt.Errorf("%w: the text did not come from the template renderer", ErrAnswer)
	}
	return Text{s: r.Text}, nil
}

func strs(ts []Text) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.s)
	}
	return out
}

// ErrAnswer is returned for an input that cannot become an answer that satisfies the contract. It names the part and never carries the text (AGENT-13).
var ErrAnswer = errors.New("answer: the input does not satisfy the customer-answer contract")

// Verdict is one Outcome verdict of the investigation: the outcome it is about (the pack's ID) and its result.
type Verdict struct {
	OutcomeID string
	Result    contracts.EnumsV1Verdict
}

// NotKnown is one thing that is not known yet, and why.
type NotKnown struct{ What, Why Text }

// Input is everything the answer needs. The texts are the renderer's sentences (Text; A3); Build checks them against the contract and changes none.
type Input struct {
	Found    []Text
	NotKnown []NotKnown
	Summary  Text
	Do       []Text
	DoNot    []Text
	Next     Text
	When     *Text
	// Verdicts are the investigation's verdicts. They decide `done.verified` and the case reference; nothing else does.
	Verdicts []Verdict
	// CaseOpenedOutcomeID is the pack's ID of the case-opened outcome. With none, no case reference is ever shown.
	CaseOpenedOutcomeID string
	// CaseRefMasked is the case reference, already masked. It is shown only after the case-opened outcome has an ACHIEVED verdict.
	CaseRefMasked string
}

// Verified is A237: at least one verdict, and every verdict ACHIEVED.
func Verified(vs []Verdict) bool {
	if len(vs) == 0 {
		return false
	}
	for _, v := range vs {
		if v.Result != contracts.EnumsV1VerdictACHIEVED {
			return false
		}
	}
	return true
}

// allAchieved is true when no verdict is anything but ACHIEVED. With no verdict it is true: there is no outcome to be unsure of, and `done.verified` is false by A237.
func allAchieved(vs []Verdict) bool {
	for _, v := range vs {
		if v.Result != contracts.EnumsV1VerdictACHIEVED {
			return false
		}
	}
	return true
}

// caseOpenedAchieved is true when the outcome has at least one verdict and every verdict of it is ACHIEVED. With no outcome ID configured it is false.
func caseOpenedAchieved(vs []Verdict, outcomeID string) bool {
	if outcomeID == "" {
		return false
	}
	var of []Verdict
	for _, v := range vs {
		if v.OutcomeID == outcomeID {
			of = append(of, v)
		}
	}
	return Verified(of)
}

// Build makes the answer, or refuses with ErrAnswer. An outcome that is not ACHIEVED must be said: any such verdict needs at least one not_known item (SR on #618).
func Build(in Input) (contracts.CustomerAnswerV1, error) {
	a := contracts.CustomerAnswerV1{
		SchemaVersion: schemaVersion,
		Found:         strs(in.Found),
		Done:          contracts.CustomerAnswerV1Done{Summary: in.Summary.s, Verified: Verified(in.Verdicts)},
		Advice:        contracts.CustomerAnswerV1Advice{Do: strs(in.Do), DoNot: strs(in.DoNot)},
		Next:          contracts.CustomerAnswerV1Next{What: in.Next.s},
	}
	if in.When != nil {
		w := in.When.s
		a.Next.When = &w
	}
	for _, n := range in.NotKnown {
		a.NotKnown = append(a.NotKnown, contracts.CustomerAnswerV1NotKnownItem{What: n.What.s, Why: n.Why.s})
	}
	if in.CaseRefMasked != "" && caseOpenedAchieved(in.Verdicts, in.CaseOpenedOutcomeID) {
		ref := in.CaseRefMasked
		a.CaseRefMasked = &ref
	}
	if !allAchieved(in.Verdicts) && len(a.NotKnown) == 0 {
		return contracts.CustomerAnswerV1{}, fmt.Errorf("%w: a verdict that is not ACHIEVED needs a not_known item", ErrAnswer)
	}
	if err := check(a); err != nil {
		return contracts.CustomerAnswerV1{}, err
	}
	return a, nil
}

func text(part, s string) error {
	if s == "" || utf8.RuneCountInString(s) > maxText || !utf8.ValidString(s) {
		return fmt.Errorf("%w: %s is empty, too long or not text", ErrAnswer, part)
	}
	return nil
}

func texts(part string, ss []string, allowEmptyList bool, max int) error {
	if len(ss) > max || (!allowEmptyList && len(ss) == 0) {
		return fmt.Errorf("%w: %s has %d items", ErrAnswer, part, len(ss))
	}
	for _, s := range ss {
		if err := text(part, s); err != nil {
			return err
		}
	}
	return nil
}

// check holds the answer to the contract: the bounds, the mask of a case reference, and no digit run over the Record's limit in any member (payloadguard's definition, A194).
func check(a contracts.CustomerAnswerV1) error {
	if err := texts("found", a.Found, true, maxItems); err != nil {
		return err
	}
	if len(a.NotKnown) > maxItems {
		return fmt.Errorf("%w: not_known has %d items", ErrAnswer, len(a.NotKnown))
	}
	for _, n := range a.NotKnown {
		if err := text("not_known", n.What); err != nil {
			return err
		}
		if err := text("not_known", n.Why); err != nil {
			return err
		}
	}
	for part, s := range map[string]string{"done": a.Done.Summary, "next": a.Next.What} {
		if err := text(part, s); err != nil {
			return err
		}
	}
	if a.Next.When != nil {
		if err := text("next", *a.Next.When); err != nil {
			return err
		}
		if utf8.RuneCountInString(*a.Next.When) > maxWhen {
			return fmt.Errorf("%w: next.when is over %d characters", ErrAnswer, maxWhen)
		}
	}
	if err := texts("advice.do", a.Advice.Do, true, maxAdvice); err != nil {
		return err
	}
	if err := texts("advice.do_not", a.Advice.DoNot, true, maxAdvice); err != nil {
		return err
	}
	if a.CaseRefMasked != nil {
		r := *a.CaseRefMasked
		if utf8.RuneCountInString(r) > maxCaseRef || !strings.ContainsAny(r, "*•…") || digitCount(r) > payloadguard.Limit {
			return fmt.Errorf("%w: the case reference is not masked", ErrAnswer)
		}
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("%w: marshal", ErrAnswer)
	}
	if err := payloadguard.Check(raw); err != nil {
		return fmt.Errorf("%w: a text holds a run of digits over the limit", ErrAnswer)
	}
	return nil
}

// digitCount is the number of decimal digits of any script in s: a masked reference shows at most four in all (A187, A154).
func digitCount(s string) int {
	n := 0
	for _, r := range s {
		if isDigit(r) {
			n++
		}
	}
	return n
}

func isDigit(r rune) bool { return unicode.Is(unicode.Nd, r) }
