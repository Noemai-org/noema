// Package masking reads the longest digit run that may appear unmasked, from the pack's masking-limits.v1 document (SEC-03, REQ-SEC-003, REQ-EXP-002, AGENT-13, AGENT-16).
// It is the one source the core's redactor, the explanation validator and the error-scanner test read, and the model service reads the same file. There is no default:
// a document that is not exactly the contract is an error, and the zero Limits reports that it is not loaded. Ruling A154 as corrected (relayed, not yet committed).
//
// May import: the standard library and internal/contracts.
//
// Must never: hold a number of its own, or return a value with an error.
package masking

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// keptDigits is how many digits a mask keeps (AGENTS section 7: mask to the last four; PERD 31.2). It is the contract's minimum, not a policy value.
const keptDigits = 4

// maxRun is the contract's maximum for max_unmasked_digit_run.
const maxRun = 32

// Error is the typed error of Load: the document is not exactly masking-limits.v1. Reason never carries the document.
type Error struct {
	Reason string
	Err    error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return "masking: limits: " + e.Reason + ": " + e.Err.Error()
	}
	return "masking: limits: " + e.Reason
}

// Unwrap keeps the decode cause for errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }

func refuse(reason string, cause error) *Error { return &Error{Reason: reason, Err: cause} }

// Limits is the loaded document. Its zero value is not loaded.
type Limits struct{ maxUnmasked int }

// Loaded reports whether the Limits came from Load.
func (l Limits) Loaded() bool { return l.maxUnmasked >= keptDigits }

// MaxUnmaskedRun is the longest run of digits left unmasked. It is 0 for a Limits that was not loaded; check Loaded first.
func (l Limits) MaxUnmaskedRun() int { return l.maxUnmasked }

// MinMaskedRun is the shortest run that is masked: one more than MaxUnmaskedRun.
func (l Limits) MinMaskedRun() int { return l.maxUnmasked + 1 }

// KeptDigits is how many digits of a masked run are kept (the last ones).
func (Limits) KeptDigits() int { return keptDigits }

// packIDShape is the pattern of pack_id in masking-limits.v1.json. The generated type does not check patterns, so Load does (\z, not $).
var packIDShape = regexp.MustCompile(`\A[a-z][a-z0-9]*\z`)

// Load reads one masking-limits.v1 document. Any deviation from the contract is a *Error and the returned Limits is the zero value.
func Load(r io.Reader) (Limits, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var d contracts.MaskingLimitsV1
	if err := dec.Decode(&d); err != nil {
		return Limits{}, refuse("cannot read the document", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Limits{}, refuse("anything after the document is refused", nil)
	}
	if string(d.SchemaVersion) != "1.0" {
		return Limits{}, refuse("schema_version is not 1.0", nil)
	}
	if !packIDShape.MatchString(string(d.PackId)) {
		return Limits{}, refuse("pack_id does not match the contract", nil)
	}
	if l := string(d.Label); l != "SYNTHETIC" && l != "VALIDATED" {
		return Limits{}, refuse("label is not allowed", nil)
	}
	if d.MaxUnmaskedDigitRun < keptDigits || d.MaxUnmaskedDigitRun > maxRun {
		return Limits{}, refuse("max_unmasked_digit_run is out of range", nil)
	}
	return Limits{maxUnmasked: int(d.MaxUnmaskedDigitRun)}, nil
}
