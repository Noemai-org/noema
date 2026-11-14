package intake

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// The ports of T7, T8 and T9 (PERD 23.3). T1 to T6 never read about a target; these are the only ports that may, and each may read one thing only: T7 the candidates scoped to
// the session's principal, T8 whether that principal owns the resolved target, T9 the freshness of the sealed evidence snapshot. No step before T7 holds or calls any of them
// (REQ-TRU-002: refuse before read).

// Errors a step returns when a port answers with something Trust cannot trust. Each ends the evaluation in UNKNOWN INTERNAL_ERROR, never ALLOW (REQ-NFR-002, INV-16).
var (
	// ErrIntentView is returned when the admitted intent is missing or its view is for another version than the one evaluated.
	ErrIntentView = errors.New("intake: the admitted intent's view does not match the evaluation")
	// ErrBadDisambiguation is returned when the pack's disambiguation parameters are unusable (a limit below its lowest meaning).
	ErrBadDisambiguation = errors.New("intake: the disambiguation parameters are not usable")
	// ErrBadCandidates is returned when the candidates cannot be told apart: a candidate with no ID, or the same ID twice.
	ErrBadCandidates = errors.New("intake: the candidate set is not usable")
	// ErrNoSnapshot is returned when T9 is asked to decide with no sealed evidence snapshot and no report that evidence is unavailable.
	ErrNoSnapshot = errors.New("intake: there is no evidence snapshot to decide on")
	// ErrBadEvidence is returned when the evidence report holds a fact status outside the contract's enum, or a freshness the Record cannot hold.
	ErrBadEvidence = errors.New("intake: the evidence report is not usable")
	// ErrNoBasis is returned when T9 has no basis builder to record into.
	ErrNoBasis = errors.New("intake: there is no basis builder")
)

// RankedCandidate is one candidate for the principal's unresolved reference slot, with its score. Display and Attrs are pre-formatted and masked by the adapter
// (REQ-AMB-004, REQ-CTX-006); Trust shows them to the principal and reads nothing else about the target.
type RankedCandidate struct {
	decide.Candidate
	// Score is the candidate's score in the pack's fixed-point unit, the same unit as Disambiguation.Floor. Trust compares integers and never decides on a float.
	Score int64
}

// Disambiguation is the pack's data for resolving a reference slot (REQ-AMB-003). Trust holds none of it: ruling A205 Q1 gives its values as PROVISIONAL pack data and
// they arrive through the port. Trust does not auto-resolve (a slot is resolved by Understanding, which writes the next intent version), so the auto-resolve margin is not read here.
type Disambiguation struct {
	// Floor is the absolute floor: a candidate scoring below it is not offered.
	Floor int64
	// MaxCandidates is the most candidates a question may list (`max_candidates_before_escalate`); more above the floor is an ESCALATE. A value above platform/ceilings is refused.
	MaxCandidates int
	// MaxDigitRun is the most consecutive digits a label may show (REQ-AMB-004). A value above platform/ceilings is refused.
	MaxDigitRun int
	// AttributeOrder is the pack's fixed order of askable attributes (ask by attribute, D4).
	AttributeOrder []decide.AttributeSpec
	// OptionsKey is the pack's question key for OPTIONS mode.
	OptionsKey string
}

// TargetCandidates is what the Candidates port answers for one intent.
type TargetCandidates struct {
	Disambiguation Disambiguation
	// Ranked holds the candidates for this principal only (REQ-CTX-006: another principal's rows do not exist for the caller). Trust sorts them, so the order is not relied on.
	Ranked []RankedCandidate
}

// Candidates is the T7 read: the candidates for the admitted intent's unresolved reference slot, scoped to the session's principal, and the pack's disambiguation data. It
// is called only for an intent that is not fully resolved.
type Candidates interface {
	ScopedCandidates(ctx context.Context, tenantID, principalID string, a AdmittedIntent) (TargetCandidates, error)
}

// Ownership is the T8 read: whether the session's principal owns the resolved target (the intent's `principal_must_own` slots). It answers true for an intent with no such slot.
// A target that does not exist and a target the principal does not own are the same answer (REQ-TRU-003): the adapter returns owned=false or ErrNotFound for either, and Trust
// treats the two identically.
type Ownership interface {
	OwnsTarget(ctx context.Context, tenantID, principalID string, a AdmittedIntent) (owned bool, err error)
}

// SourceItem is one required source item of the sealed snapshot, with its freshness as the Decision Record keeps it and its fact status (PERD 21.1).
type SourceItem struct {
	Freshness decide.Freshness
	Status    contracts.EnumsV1FactStatus
}

// SnapshotEvidence is what the Evidence port reports for a sealed snapshot. It is the state after the Evidence Layer's refresh attempt (REQ-CTX-003: a refresh that fails makes
// the fact UNAVAILABLE), so an item still past its limit is one that could not be refreshed.
type SnapshotEvidence struct {
	// Items holds one entry per required source item.
	Items []SourceItem
	// RequiredUnavailable is true when a required source could not be read at all.
	RequiredUnavailable bool
}

// Evidence is the T9 read: the freshness of each required source item of the sealed snapshot the orchestrator named.
type Evidence interface {
	SnapshotFreshness(ctx context.Context, tenantID, snapshotID string) (SnapshotEvidence, error)
}
