// Package answer builds the customer's answer (customer-answer.v1) and is where the core, not the UI, decides what is verified (ruling A237, interim, relayed; noema#611; PERD 13
// rule 6 and 40.1; REQ-UI-001). `done.verified` is true only when there is at least one verdict and every verdict is ACHIEVED. `case_ref_masked` is present only when the case-opened
// outcome has an ACHIEVED verdict. The UI renders what it is given and derives nothing.
//
// May import: the standard library, internal/contracts, record/payloadguard (the one digit definition, A194).
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

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

// Bounds of the contract (customer-answer.v1): frame bounds, not policy.
const (
	maxText       = 300
	maxItems      = 8
	maxCaseRef    = 64
	schemaVersion = "1.0"
)

// ErrAnswer is returned for an input that cannot become an answer that satisfies the contract. It names the part and never carries the text (AGENT-13).
var ErrAnswer = errors.New("answer: the input does not satisfy the customer-answer contract")

// Verdict is one Outcome verdict of the investigation: the outcome it is about (the pack's ID) and its result.
type Verdict struct {
	OutcomeID string
	Result    contracts.EnumsV1Verdict
}

// NotKnown is one thing that is not known yet, and why.
type NotKnown struct{ What, Why string }

// Input is everything the answer needs. The texts are already the customer's words (the pack's templates and the masked facts); Build checks them and changes none.
type Input struct {
	Found    []string
	NotKnown []NotKnown
	Summary  string
	Do       []string
	DoNot    []string
	Next     string
	When     *string
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

// Build makes the answer, or refuses with ErrAnswer.
func Build(in Input) (contracts.CustomerAnswerV1, error) {
	a := contracts.CustomerAnswerV1{
		SchemaVersion: schemaVersion,
		Found:         append([]string(nil), in.Found...),
		Done:          contracts.CustomerAnswerV1Done{Summary: in.Summary, Verified: Verified(in.Verdicts)},
		Advice:        contracts.CustomerAnswerV1Advice{Do: append([]string(nil), in.Do...), DoNot: append([]string(nil), in.DoNot...)},
		Next:          contracts.CustomerAnswerV1Next{What: in.Next, When: in.When},
	}
	for _, n := range in.NotKnown {
		a.NotKnown = append(a.NotKnown, contracts.CustomerAnswerV1NotKnownItem{What: n.What, Why: n.Why})
	}
	if in.CaseRefMasked != "" && caseOpenedAchieved(in.Verdicts, in.CaseOpenedOutcomeID) {
		ref := in.CaseRefMasked
		a.CaseRefMasked = &ref
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

func texts(part string, ss []string, allowEmptyList bool) error {
	if len(ss) > maxItems || (!allowEmptyList && len(ss) == 0) {
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
	if err := texts("found", a.Found, true); err != nil {
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
	}
	if err := texts("advice.do", a.Advice.Do, true); err != nil {
		return err
	}
	if err := texts("advice.do_not", a.Advice.DoNot, true); err != nil {
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
