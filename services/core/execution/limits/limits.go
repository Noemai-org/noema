package limits

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/breaker"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

const errPrefix = "limits: "

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

// Load reads one execution-limits.v1 document. Any deviation from the contract is an error; there are no defaults.
func Load(r io.Reader) (Limits, error) {
	dec := json.NewDecoder(r)
	var d contracts.ExecutionLimitsV1
	if err := dec.Decode(&d); err != nil {
		return Limits{}, fmt.Errorf("%scannot read the document: %w", errPrefix, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Limits{}, errors.New(errPrefix + "anything after the document is refused")
	}
	cb := d.CircuitBreaker
	if cb.MinSamples < 1 || cb.MinSamples > math.MaxInt32 {
		return Limits{}, errors.New(errPrefix + "min_samples is out of range")
	}
	if cb.CooldownS < 1 || cb.CooldownS > int64(math.MaxInt64/int64(time.Second)) {
		return Limits{}, errors.New(errPrefix + "cooldown_s is out of range")
	}
	if cb.HalfOpenProbes != 1 {
		return Limits{}, errors.New(errPrefix + "the breaker supports exactly one half-open probe")
	}
	b := d.DispatchBudget
	if b.MaxDispatches < 1 || b.MaxDispatches > math.MaxInt32 {
		return Limits{}, errors.New(errPrefix + "max_dispatches is out of range")
	}
	if len(b.Key) != len(wantKey) {
		return Limits{}, errors.New(errPrefix + "the budget key is not tenant_id, correlation_id, capability, target_ref")
	}
	for i, k := range b.Key {
		if k != wantKey[i] {
			return Limits{}, errors.New(errPrefix + "the budget key is not tenant_id, correlation_id, capability, target_ref")
		}
	}
	return Limits{
		Breaker:        breaker.Policy{MinSamples: int(cb.MinSamples), Cooldown: time.Duration(cb.CooldownS) * time.Second},
		DispatchBudget: int(b.MaxDispatches),
	}, nil
}
