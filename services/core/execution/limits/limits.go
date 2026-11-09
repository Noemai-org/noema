package limits

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/breaker"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

const errPrefix = "limits: "

// Code is an error code of /contracts/schemas/errors.v2.json.
type Code string

// CodeSchemaInvalid is the code of every refusal: the document is not exactly execution-limits.v1.
const CodeSchemaInvalid Code = "SCHEMA_INVALID"

// Error is the typed error of Load. Reason is for people and logs and never carries the document; Err is the underlying cause, if any.
type Error struct {
	Code   Code
	Reason string
	Err    error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return errPrefix + string(e.Code) + ": " + e.Reason + ": " + e.Err.Error()
	}
	return errPrefix + string(e.Code) + ": " + e.Reason
}

// Unwrap keeps the decode cause for errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }

func refuse(reason string, cause error) *Error {
	return &Error{Code: CodeSchemaInvalid, Reason: reason, Err: cause}
}

// Limits is what Execution reads from the pack.
type Limits struct {
	// Breaker is the policy every circuit breaker is built with. Cooldown runs on the injected clock (INV-21).
	Breaker breaker.Policy
	// DispatchBudget is the number of DISPATCH_ATTEMPTED events allowed per tenant_id, correlation_id, capability and target_ref.
	DispatchBudget int
}

// wantKey is the budget key of ruling A3, in order.
var wantKey = []contracts.ExecutionLimitsV1DispatchBudgetKeyItem{
	contracts.ExecutionLimitsV1DispatchBudgetKeyItemTenantId,
	contracts.ExecutionLimitsV1DispatchBudgetKeyItemCorrelationId,
	contracts.ExecutionLimitsV1DispatchBudgetKeyItemCapability,
	contracts.ExecutionLimitsV1DispatchBudgetKeyItemTargetRef,
}

// packIDShape is the pattern of pack_id in execution-limits.v1.json. The generated type does not check patterns, so Load does.
var packIDShape = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Load reads one execution-limits.v1 document. Any deviation from the contract is an error; there are no defaults.
func Load(r io.Reader) (Limits, error) {
	dec := json.NewDecoder(r)
	var d contracts.ExecutionLimitsV1
	if err := dec.Decode(&d); err != nil {
		return Limits{}, refuse("cannot read the document", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Limits{}, refuse("anything after the document is refused", nil)
	}
	if !packIDShape.MatchString(string(d.PackId)) {
		return Limits{}, refuse("pack_id does not match the contract", nil)
	}
	cb := d.CircuitBreaker
	if cb.MinSamples < 1 || cb.MinSamples > math.MaxInt32 {
		return Limits{}, refuse("min_samples is out of range", nil)
	}
	if cb.CooldownS < 1 || cb.CooldownS > int64(math.MaxInt64/int64(time.Second)) {
		return Limits{}, refuse("cooldown_s is out of range", nil)
	}
	if cb.HalfOpenProbes != 1 {
		return Limits{}, refuse("the breaker supports exactly one half-open probe", nil)
	}
	b := d.DispatchBudget
	if b.MaxDispatches < 1 || b.MaxDispatches > math.MaxInt32 {
		return Limits{}, refuse("max_dispatches is out of range", nil)
	}
	if len(b.Key) != len(wantKey) {
		return Limits{}, refuse("the budget key is not tenant_id, correlation_id, capability, target_ref", nil)
	}
	for i, k := range b.Key {
		if k != wantKey[i] {
			return Limits{}, refuse("the budget key is not tenant_id, correlation_id, capability, target_ref", nil)
		}
	}
	return Limits{
		Breaker:        breaker.Policy{MinSamples: int(cb.MinSamples), Cooldown: time.Duration(cb.CooldownS) * time.Second},
		DispatchBudget: int(b.MaxDispatches),
	}, nil
}
