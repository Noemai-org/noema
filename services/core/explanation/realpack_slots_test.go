package explanation_test

// EXP-04, ruling A155 (the Founder's approval of the slot syntax in the pack; the slot inventory is noema#586; REQ-EXP-001, REQ-EXP-002, INV-13, AGENT-17; PERD 13 and 31.2):
// the REAL pack's en-NG templates hold the `{fact:<kind>}` slots of their outcome's group, fill only from the one KNOWN fact of each kind, and are blocked, never guessed, when a fact
// is missing or is not KNOWN. SYNTHETIC facts.
//
// Written before the pack edit: these fail while the templates are still slot-free.

import (
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

const exception = "fin.outcome.exception."

// groupKinds is the slot inventory of noema#586 as approved: the fact kinds each outcome's templates hold, in the order the sentence holds them. Kinds the inventory marked [REC]
// (payment.counterparty, case.team, case.clock) are not in the pack yet.
var groupKinds = map[string][]string{
	exception + "status_explained":  {"payment.amount", "account", "payment.time"},
	exception + "case_opened":       {"case.ref"},
	exception + "case_routed":       {"case.ref"},
	exception + "complaint_logged":  {"case.ref"},
	exception + "evidence_recorded": {"case.ref"},
}

var factIDOf = map[string]string{"payment.amount": "f1", "account": "f2", "payment.time": "f3", "case.ref": "f4"}

func TestEveryRealTemplateHoldsTheSlotsOfItsOutcomeAndFillsFromKnownFacts(t *testing.T) {
	tpl := load(t)
	keys := tpl.Keys("en-NG")
	if len(keys) != 35 {
		t.Fatalf("%d templates, want 35 (5 outcomes by 7 classifications)", len(keys))
	}
	for _, key := range keys {
		outcome, class, _ := strings.Cut(key, "/")
		kinds, ok := groupKinds[outcome]
		if !ok {
			t.Fatalf("%s: an outcome the inventory does not cover", key)
		}
		f := facts(outcome, class)
		r, err := explanation.Fallback(tpl, f)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		var want []string
		for _, k := range kinds {
			want = append(want, factIDOf[k])
		}
		if strings.Join(r.FactIDs, ",") != strings.Join(want, ",") {
			t.Errorf("%s: the template used facts %v, want %v (the slots of its outcome, in order)", key, r.FactIDs, want)
		}
		if strings.ContainsAny(r.Text, "{}") {
			t.Errorf("%s: a brace is left in the text: %q", key, r.Text)
		}
		if problems := explanation.Validate(tpl, r, f, nil); len(problems) != 0 {
			t.Errorf("%s: the filled text fails the validator: %v", key, problems)
		}
	}
}

// A slot with no KNOWN fact of its kind blocks the render: a missing fact, an INFERRED one and two of one kind are each ErrSlot (A155), for every real template.
func TestAMissingOrNotKnownOrAmbiguousFactBlocksEveryRealTemplateThatNeedsIt(t *testing.T) {
	tpl := load(t)
	for _, key := range tpl.Keys("en-NG") {
		outcome, class, _ := strings.Cut(key, "/")
		for _, kind := range groupKinds[outcome] {
			without := facts(outcome, class)
			var kept []contracts.ExplanationFactsV2FactsItem
			for _, f := range without.Facts {
				if f.Kind != kind {
					kept = append(kept, f)
				}
			}
			without.Facts = kept
			if _, err := explanation.Fallback(tpl, without); !errors.Is(err, explanation.ErrSlot) {
				t.Errorf("%s: with no %s fact the render must be ErrSlot, got %v", key, kind, err)
			}
			inferred := facts(outcome, class)
			for i := range inferred.Facts {
				if inferred.Facts[i].Kind == kind {
					inferred.Facts[i].FactStatus = contracts.ExplanationFactsV2FactsItemFactStatusINFERRED
					rule := "rule_x"
					inferred.Facts[i].RuleId = &rule
				}
			}
			if _, err := explanation.Fallback(tpl, inferred); !errors.Is(err, explanation.ErrSlot) {
				t.Errorf("%s: with an INFERRED %s fact the render must be ErrSlot, got %v", key, kind, err)
			}
			twice := facts(outcome, class)
			for _, f := range twice.Facts {
				if f.Kind == kind {
					f.FactId += "b"
					twice.Facts = append(twice.Facts, f)
					break
				}
			}
			if _, err := explanation.Fallback(tpl, twice); !errors.Is(err, explanation.ErrSlot) {
				t.Errorf("%s: with two %s facts the render must be ErrSlot, got %v", key, kind, err)
			}
		}
	}
}
