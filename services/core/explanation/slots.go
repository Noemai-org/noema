package explanation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// ErrSlot means a template's {fact:<kind>} slot cannot be filled from exactly one KNOWN fact of that kind (ruling A155). The caller blocks the explanation; nothing is
// guessed, and the error message names the kind but never a value.
var ErrSlot = errors.New("explanation: a template slot cannot be filled")

// slotRe is the slot syntax approved by A155: {fact:<kind>}, where kind is a fact kind of explanation-facts.v2 (lower-case words joined by "_" and ".", as in
// "payment.amount" or "account").
var slotRe = regexp.MustCompile(`\{fact:([a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)*)\}`)

// checkSlots refuses a template that has a brace outside a well-formed slot, so an unresolved placeholder can never reach a customer.
func checkSlots(text string) error {
	if strings.ContainsAny(slotRe.ReplaceAllString(text, ""), "{}") {
		return errors.New("a brace that is not part of a well-formed {fact:<kind>} slot")
	}
	return nil
}

// expand fills each slot of a template text with the value_masked of the one KNOWN fact of the slot's kind, and returns the text and the IDs of the facts used, in slot
// order. Zero facts of the kind, more than one, one that is not KNOWN, an empty value, or a value with a brace is ErrSlot. The values are inserted once and not
// expanded again.
func expand(text string, f contracts.ExplanationFactsV2) (string, []string, error) {
	var used []string
	var failure error
	out := slotRe.ReplaceAllStringFunc(text, func(slot string) string {
		kind := slotRe.FindStringSubmatch(slot)[1]
		var found *contracts.ExplanationFactsV2FactsItem
		n := 0
		for i := range f.Facts {
			if f.Facts[i].Kind == kind {
				found = &f.Facts[i]
				n++
			}
		}
		switch {
		case failure != nil:
		case n == 0:
			failure = fmt.Errorf("%w: no fact of kind %q", ErrSlot, kind)
		case n > 1:
			failure = fmt.Errorf("%w: %d facts of kind %q, so the slot is ambiguous", ErrSlot, n, kind)
		case found.FactStatus != contracts.ExplanationFactsV2FactsItemFactStatusKNOWN:
			failure = fmt.Errorf("%w: the fact of kind %q is not KNOWN (INV-13)", ErrSlot, kind)
		case strings.TrimSpace(found.ValueMasked) == "" || strings.ContainsAny(found.ValueMasked, "{}"):
			failure = fmt.Errorf("%w: the fact of kind %q has no usable value", ErrSlot, kind)
		default:
			used = append(used, found.FactId)
			return found.ValueMasked
		}
		return ""
	})
	if failure != nil {
		return "", nil, failure
	}
	return out, used, nil
}
