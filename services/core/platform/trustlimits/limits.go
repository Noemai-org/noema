// Package trustlimits reads the two provisional limits the Trust steps use, from the pack's trust-limits.v1 document: the ambiguity absolute floor and the clock AT_RISK threshold
// (TRU-06, REQ-AMB-003, REQ-TRU-006, AGENT-16; Architect ruling A205 Q1). Both are integer basis points (0 to 10000, A166). Both values are NON-CANONICAL until GOV-06 adopts them.
// There is no default: a document that is not exactly the contract is an error, and the zero Limits reports that it is not loaded.
//
// May import: the standard library and internal/contracts.
//
// Must never: hold a threshold of its own, default a missing member, use a float, return a value with an error, or read the clock.
package trustlimits

import (
	"io"
	"regexp"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// Both members are BasisPoints in trust-limits.v1: an integer from 0 to 10000 (intent-slot.v2 $defs/BasisPoints). The generated type does not check values, so Load does.
const (
	minBasisPoints = 0
	maxBasisPoints = 10000
)

// maxDocumentBytes bounds what Load reads; the contract is five short members.
const maxDocumentBytes = 1 << 16

// Error is the typed error of Load: the document is not exactly trust-limits.v1. Reason never carries the document.
type Error struct {
	Reason string
	Err    error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return "trustlimits: " + e.Reason + ": " + e.Err.Error()
	}
	return "trustlimits: " + e.Reason
}

// Unwrap keeps the decode cause for errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }

func refuse(reason string, cause error) *Error { return &Error{Reason: reason, Err: cause} }

// Limits is the loaded document. Its zero value is not loaded.
type Limits struct {
	floor, atRisk int
	loaded        bool
}

// Loaded reports whether the Limits came from Load.
func (l Limits) Loaded() bool { return l.loaded }

// AmbiguityAbsoluteFloorBP is the least top score, in basis points, at which the auto-resolve rule may resolve (PERD v1.23 line 1772). It is 0 when not loaded; check Loaded first.
func (l Limits) AmbiguityAbsoluteFloorBP() int { return l.floor }

// ClockAtRiskRemainingBP is the share of a clock's duration, in basis points, under which the clock is AT_RISK (PERD v1.23 line 2134). It is 0 when not loaded; check Loaded first.
func (l Limits) ClockAtRiskRemainingBP() int { return l.atRisk }

// packIDShape is the pattern of pack_id in trust-limits.v1.json. The generated type does not check patterns, so Load does (\z, not $).
var packIDShape = regexp.MustCompile(`\A[a-z][a-z0-9]*\z`)

// Load reads one trust-limits.v1 document. Any deviation from the contract is a *Error and the returned Limits is the zero value.
func Load(r io.Reader) (Limits, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxDocumentBytes+1))
	if err != nil {
		return Limits{}, refuse("cannot read the document", err)
	}
	if len(b) > maxDocumentBytes {
		return Limits{}, refuse("the document is larger than the contract allows", nil)
	}
	var d contracts.TrustLimitsV1
	if err := d.UnmarshalJSON(b); err != nil {
		return Limits{}, refuse("not exactly the contract", err)
	}
	if !packIDShape.MatchString(d.PackId) {
		return Limits{}, refuse("pack_id does not match the contract", nil)
	}
	floor, err := basisPoints("ambiguity_absolute_floor_bp", int64(d.AmbiguityAbsoluteFloorBp))
	if err != nil {
		return Limits{}, err
	}
	atRisk, err := basisPoints("clock_at_risk_remaining_bp", int64(d.ClockAtRiskRemainingBp))
	if err != nil {
		return Limits{}, err
	}
	return Limits{floor: floor, atRisk: atRisk, loaded: true}, nil
}

func basisPoints(member string, v int64) (int, error) {
	if v < minBasisPoints || v > maxBasisPoints {
		return 0, refuse(member+" is outside basis points (an integer from 0 to 10000)", nil)
	}
	return int(v), nil
}
