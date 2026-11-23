package main

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	ownershipport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/ownership"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

// compose_trustinputs.go builds the six Trust inputs of lane 2 of the G1 final composition (STM-01; A537, interim, relayed, not yet committed): Ownership, Writes, Evidence, Reconciler,
// Policies and Clocks. REQ-STM-001, REQ-TRU-002; INV-14, INV-16, INV-19; AGENT-16. RED STUB: it builds nothing yet.

// trustInputsIn is everything the builder takes: the verified pack (the trustpack.Pack and the verified bytes), the per-tenant Trust logins, the Understanding parts that main already
// builds (the rule set and the evidence mapping), the screened payload parts and the enterprise read. A part that is absent leaves the components that need it a named gap.
type trustInputsIn struct {
	Pack            *trustpack.Pack
	Verified        packload.Pack
	Tenants         []string
	Pools           map[string]*pgxpool.Pool
	Timeout         time.Duration
	Clock           clock.Clock
	RuleSet         *reconcile.RuleSet
	Mapping         *mapping.Mapping
	Payloads        payloadread.PayloadReader
	Screen          ScreenDeps
	ListEntities    map[string]intakesources.ListSpec
	OwnershipReader ownershipport.ScopedReader
}

// trustInputs is what the builder made. A nil port is a gap, named in Gaps by its TrustAdapters field name.
type trustInputs struct {
	Ownership intake.Ownership
	Evidence  intake.Evidence
	Policies  intake.Policies
	Gaps      []string
}

func buildTrustInputs(trustInputsIn) (trustInputs, error) { return trustInputs{}, nil }

// fill sets the adapters that were built and leaves every other field of a as it was.
func (t trustInputs) fill(a TrustAdapters) TrustAdapters { return a }
