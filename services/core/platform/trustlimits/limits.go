// Package trustlimits reads the two provisional limits the Trust steps use, from the pack's trust-limits.v2 document: the ambiguity absolute floor and the clock AT_RISK threshold
// (TRU-06, REQ-AMB-003, REQ-TRU-006, AGENT-16; Architect ruling A205 Q1). Both are integer basis points (0 to 10000, A166). Both values are NON-CANONICAL until GOV-06 adopts them.
// There is no default: a document that is not exactly the contract is an error, and the zero Limits is not loaded and its accessors refuse.
//
// May import: the standard library, internal/contracts and platform/ceilings.
//
// Must never: hold a threshold of its own, default a missing member, use a float, return a limit of a document it refused, quote the document in an error text, or read the clock.
package trustlimits

import (
	"errors"
	"io"
	"regexp"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/ceilings"
)

// Both members are BasisPoints in trust-limits.v2: an integer from 0 to 10000 (intent-slot.v2 $defs/BasisPoints). The generated type does not check values, so Load does, against
// the bounds of ruling A233 that platform/ceilings holds: the ambiguity floor is at least 7000 (it may only tighten), the AT_RISK share is 1000 to 5000. Zero is below every
// bound: a floor of 0 passes every candidate and an AT_RISK threshold of 0 means a clock is never AT_RISK, so a pack could switch a safety limit off (Security Reviewer on noema#574).
const maxBasisPoints = 10000

// maxDocumentBytes bounds what Load reads; the contract is five short members.
const maxDocumentBytes = 1 << 16

// Error is the typed error of Load: the document is not exactly trust-limits.v2. Reason never carries the document.
type Error struct {
	Reason string
	Err    error
}

// Error is the reason only: a decode cause quotes the bad value, and an error text may be shown outside; the cause stays reachable through Unwrap.
func (e *Error) Error() string { return "trustlimits: " + e.Reason }

// Unwrap keeps the decode cause for errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }

func refuse(reason string, cause error) *Error { return &Error{Reason: reason, Err: cause} }

// Limits is the loaded document. Its zero value is not loaded.
type Limits struct {
	floor, atRisk int
	confirmCeilMS int
	label         string
	loaded        bool
}

// ErrNotLoaded is returned by an accessor of a Limits that did not come from Load.
var ErrNotLoaded = errors.New("trustlimits: the limits are not loaded")

// Label is the document's label (SYNTHETIC or VALIDATED): the values are provisional and NON-CANONICAL while it is SYNTHETIC (A205 Q1). Empty when not loaded.
func (l Limits) Label() string { return l.label }

// Loaded reports whether the Limits came from Load.
func (l Limits) Loaded() bool { return l.loaded }

// AmbiguityAbsoluteFloorBP is the least top score, in basis points, at which the auto-resolve rule may resolve (PERD v1.23 line 1772). A Limits that was not loaded
// returns ErrNotLoaded, never a value: a zero would be the most permissive floor.
func (l Limits) AmbiguityAbsoluteFloorBP() (int, error) {
	if !l.loaded {
		return 0, ErrNotLoaded
	}
	return l.floor, nil
}

// ClockAtRiskRemainingBP is the share of a clock's duration, in basis points, under which the clock is AT_RISK (PERD v1.23 line 2134). A Limits that was not loaded
// returns ErrNotLoaded, never a value: a zero would mean a clock is never AT_RISK.
func (l Limits) ClockAtRiskRemainingBP() (int, error) {
	if !l.loaded {
		return 0, ErrNotLoaded
	}
	return l.atRisk, nil
}

// ConfirmationTTLCeilingMS is the most, in milliseconds, that the time-to-live of a confirmation request may be (ruling A561, interim; PERD v1.23 section 67, "confirmation TTL
// 10 min", adopted by DEC-2026-10-17). The intake and the Writer refuse a larger TTL. A Limits that was not loaded returns ErrNotLoaded, never a value: a zero would refuse every TTL
// and a missing ceiling must never read as no ceiling.
func (l Limits) ConfirmationTTLCeilingMS() (int, error) {
	if !l.loaded {
		return 0, ErrNotLoaded
	}
	return l.confirmCeilMS, nil
}

// packIDShape is the pattern of pack_id in trust-limits.v2.json. The generated type does not check patterns, so Load does (\z, not $).
var packIDShape = regexp.MustCompile(`\A[a-z][a-z0-9]*\z`)

// Load reads one trust-limits.v2 document for the pack named by wantPackID (the pack the caller is loading for, never taken from the document). Any deviation from the
// contract, a document of another pack included, is a *Error and the returned Limits is the zero value.
func Load(r io.Reader, wantPackID string) (Limits, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxDocumentBytes+1))
	if err != nil {
		return Limits{}, refuse("cannot read the document", err)
	}
	if len(b) > maxDocumentBytes {
		return Limits{}, refuse("the document is larger than the contract allows", nil)
	}
	var d contracts.TrustLimitsV2
	if err := d.UnmarshalJSON(b); err != nil {
		return Limits{}, refuse("not exactly the contract", err)
	}
	if !packIDShape.MatchString(d.PackId) {
		return Limits{}, refuse("pack_id does not match the contract", nil)
	}
	if !packIDShape.MatchString(wantPackID) || d.PackId != wantPackID {
		return Limits{}, refuse("the document is not the one of the pack asked for", nil)
	}
	if err := checkRouting(d); err != nil {
		return Limits{}, err
	}
	floor, err := boundedInt("ambiguity_absolute_floor_bp", int64(d.AmbiguityAbsoluteFloorBp), ceilings.MinAmbiguityAbsoluteFloorBP, maxBasisPoints)
	if err != nil {
		return Limits{}, err
	}
	atRisk, err := boundedInt("clock_at_risk_remaining_bp", int64(d.ClockAtRiskRemainingBp), ceilings.MinClockAtRiskRemainingBP, ceilings.MaxClockAtRiskRemainingBP)
	if err != nil {
		return Limits{}, err
	}
	ceil, err := boundedInt("confirmation_ttl_ceiling_ms", d.ConfirmationTtlCeilingMs, 1, ceilings.MaxConfirmationTTLMillis)
	if err != nil {
		return Limits{}, err
	}
	return Limits{floor: floor, atRisk: atRisk, confirmCeilMS: ceil, label: string(d.Label), loaded: true}, nil
}

// boundedInt checks an integer member of the document against the bounds the rulings fix (platform/ceilings).
func boundedInt(member string, v, least, most int64) (int, error) {
	if v < least || v > most {
		return 0, refuse(member+" is outside the bounds of the ruling (A233, A561)", nil)
	}
	return int(v), nil
}
