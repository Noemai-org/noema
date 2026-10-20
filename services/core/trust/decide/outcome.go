// Package decide builds Trust's one decision per evaluation, with constructors that make forbidden decisions
// unrepresentable: an intent that is not fully resolved can only be clarified or escalated, and an ALLOW needs all three
// authority layers to pass (REQ-AMB-001, REQ-TRU-001). It also builds the clarification question by attribute (D4,
// REQ-AMB-004) and binds an answer to a candidate set (REQ-AMB-005).
//
// Implements REQ-AMB-001, REQ-AMB-004, REQ-AMB-005, REQ-TRU-001, REQ-TRU-002 (the gate steps T1 to T6).
//
// May import: the standard library.
//
// Must never: call a model, read the clock (the caller passes now), read a store, hold a limit or a threshold (limits
// arrive as arguments from pack and platform configuration), name an industry term, or build a decision through any path
// but the typed constructors.
package decide

import "errors"

// Outcome is one of the seven Trust outcomes (PERD §23.1).
type Outcome string

// The seven outcomes.
const (
	Allow    Outcome = "ALLOW"
	Clarify  Outcome = "CLARIFY"
	Confirm  Outcome = "CONFIRM"
	Wait     Outcome = "WAIT"
	Escalate Outcome = "ESCALATE"
	Reject   Outcome = "REJECT"
	Unknown  Outcome = "UNKNOWN"
)

// LayerResult is the result of one authorisation layer. Nothing else is allowed (PERD §23.2).
type LayerResult string

// The three layer results.
const (
	Pass         LayerResult = "PASS"
	Fail         LayerResult = "FAIL"
	NotEvaluated LayerResult = "NOT_EVALUATED"
)

// Authority records the three layers (SoT §43.2).
type Authority struct{ Technical, Policy, Business LayerResult }

// AllPass reports whether every layer is PASS. An ALLOW is invalid otherwise (fail-closed, DEC-2026-10-18).
func (a Authority) AllPass() bool {
	return a.Technical == Pass && a.Policy == Pass && a.Business == Pass
}

func (a Authority) valid() bool {
	for _, l := range []LayerResult{a.Technical, a.Policy, a.Business} {
		if l != Pass && l != Fail && l != NotEvaluated {
			return false
		}
	}
	return true
}

// Errors returned by constructors. Callers compare with errors.Is.
var (
	ErrAuthorityNotPass = errors.New("decide: an ALLOW or CONFIRM needs all three authority layers at PASS")
	ErrAuthorityInvalid = errors.New("decide: an authority layer holds a value other than PASS, FAIL or NOT_EVALUATED")
	ErrNoReason         = errors.New("decide: this outcome needs at least one reason code")
	ErrZeroIntent       = errors.New("decide: the intent was not produced by Admit")
	ErrBadGateStep      = errors.New("decide: this gate step cannot produce that outcome or reason")
	ErrNoReevaluateAt   = errors.New("decide: WAIT needs a re-evaluation time")
)
