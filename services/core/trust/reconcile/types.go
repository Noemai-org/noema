// Package reconcile derives one classification of a transaction from a sealed set of evidence, by a versioned rule set
// that is pack data. It is a pure function: reconcile(evidence_set, ruleset_version, clock) gives the classification, the
// rule that fired, the evidence that contributed, any contradictions found and the time until which the answer holds
// (REQ-RES-001). Incompatible facts are never averaged (SoT §14): a contradiction is a classification of its own.
//
// Implements REQ-RES-001 and REQ-RES-002 (the contradiction freeze gate).
//
// May import: the standard library and internal/predicate.
//
// Must never: read the clock (the caller passes now), call a model, touch a store, name an industry term (rule sets
// carry them as data), treat absence as a negative fact (INV-19), or average conflicting evidence.
package reconcile

import "github.com/Noemai-org/noema/services/core/internal/predicate"

// Classification is Noema's own derived state for a transaction (PERD §22.3). The set is closed.
type Classification string

// The twelve classifications of PERD §22.3.
const (
	SuccessConfirmed             Classification = "SUCCESS_CONFIRMED"
	RejectedConfirmed            Classification = "REJECTED_CONFIRMED"
	DebitPostedCreditUnconfirmed Classification = "DEBIT_POSTED_CREDIT_UNCONFIRMED"
	SettlementPending            Classification = "SETTLEMENT_PENDING"
	ReversalPending              Classification = "REVERSAL_PENDING"
	ReversedConfirmed            Classification = "REVERSED_CONFIRMED"
	PossibleDuplicate            Classification = "POSSIBLE_DUPLICATE"
	Partial                      Classification = "PARTIAL"
	ExecutionOutcomeUnknown      Classification = "EXECUTION_OUTCOME_UNKNOWN"
	ContradictoryState           Classification = "CONTRADICTORY_STATE"
	EventOrderUncertain          Classification = "EVENT_ORDER_UNCERTAIN"
	InsufficientEvidence         Classification = "INSUFFICIENT_EVIDENCE"
)

var allClassifications = []Classification{SuccessConfirmed, RejectedConfirmed, DebitPostedCreditUnconfirmed, SettlementPending,
	ReversalPending, ReversedConfirmed, PossibleDuplicate, Partial, ExecutionOutcomeUnknown, ContradictoryState,
	EventOrderUncertain, InsufficientEvidence}

// IsTerminal reports whether a classification ends the question about the money (PERD §22.3, REQ-STM-002).
func (c Classification) IsTerminal() bool {
	return c == SuccessConfirmed || c == RejectedConfirmed || c == ReversedConfirmed
}

// FactStatus is how well a piece of evidence is known (PERD §21.1). Only KNOWN evidence can satisfy a rule.
type FactStatus string

// The eight fact statuses.
const (
	Known         FactStatus = "KNOWN"
	Inferred      FactStatus = "INFERRED"
	Ambiguous     FactStatus = "AMBIGUOUS"
	Unavailable   FactStatus = "UNAVAILABLE"
	Stale         FactStatus = "STALE"
	Contradictory FactStatus = "CONTRADICTORY"
	Unknown       FactStatus = "UNKNOWN"
	Unauthorized  FactStatus = "UNAUTHORIZED"
)

var allStatuses = []string{string(Known), string(Inferred), string(Ambiguous), string(Unavailable), string(Stale),
	string(Contradictory), string(Unknown), string(Unauthorized)}

// Evidence is one item of a sealed evidence set. Entity names the kind of fact (a rule set declares them); Fields are
// the values the pack's descriptors define. ExpiresAt is the end of the item's effective freshness (read time plus the
// effective limit), so ValidUntil needs no clock.
type Evidence struct {
	ID          string
	Source      string
	Entity      string
	Status      FactStatus
	EffectiveAt int64
	ExpiresAt   int64
	Required    bool
	Fields      map[string]predicate.Value
}

// Contradiction names two items of evidence that the rule set's compatibility matrix says cannot both hold.
type Contradiction struct {
	Label string
	A, B  string
}

// Result is the output of Reconcile. RuleID is "rule_id@version", or the rule set's fallback when no rule matched.
type Result struct {
	Classification Classification
	RuleID         string
	Contributing   []string
	Contradictions []Contradiction
	ValidUntil     int64
}
