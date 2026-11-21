package session

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// Code is an error code of /contracts/schemas/errors.v2.json.
type Code string

// CodeSchemaInvalid is the code of every refusal of LoadLimits: the document is not exactly session-limits.v1.
const CodeSchemaInvalid Code = "SCHEMA_INVALID"

// LimitsError is the typed error of LoadLimits. Reason is for people and logs and never carries the document; Err is the cause, if any.
type LimitsError struct {
	Code   Code
	Reason string
	Err    error
}

func (e *LimitsError) Error() string {
	if e.Err != nil {
		return "session: limits: " + string(e.Code) + ": " + e.Reason + ": " + e.Err.Error()
	}
	return "session: limits: " + string(e.Code) + ": " + e.Reason
}

// Unwrap keeps the decode cause for errors.Is and errors.As.
func (e *LimitsError) Unwrap() error { return e.Err }

func refuseLimits(reason string, cause error) *LimitsError {
	return &LimitsError{Code: CodeSchemaInvalid, Reason: reason, Err: cause}
}

// Limits is what the verifier's Config reads from the pack (ruling A21).
type Limits struct {
	// ClockSkew is the leeway on iat and exp, on the injected clock (INV-21). Pass it as Config.Skew.
	ClockSkew time.Duration
}

// packIDShape is the pattern of pack_id in session-limits.v1.json. The generated type does not check patterns, so LoadLimits does.
var packIDShape = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// LoadLimits reads one session-limits.v1 document. Any deviation from the contract is a *LimitsError with the SCHEMA_INVALID code; there are
// no defaults (AGENT-16).
func LoadLimits(r io.Reader) (Limits, error) {
	dec := json.NewDecoder(r)
	var d contracts.SessionLimitsV1
	if err := dec.Decode(&d); err != nil {
		return Limits{}, refuseLimits("cannot read the document", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Limits{}, refuseLimits("anything after the document is refused", nil)
	}
	if !packIDShape.MatchString(string(d.PackId)) {
		return Limits{}, refuseLimits("pack_id does not match the contract", nil)
	}
	if d.ClockSkewS < 0 || d.ClockSkewS > int64(math.MaxInt64/int64(time.Second)) {
		return Limits{}, refuseLimits("clock_skew_s is out of range", nil)
	}
	return Limits{ClockSkew: time.Duration(d.ClockSkewS) * time.Second}, nil
}
