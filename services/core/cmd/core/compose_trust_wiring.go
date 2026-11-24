package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/plangraphs"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	ownershipport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/ownership"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// trustWiringIn is everything the final composition hands to the Trust inputs and the decisions (the red stub: it builds nothing).
type trustWiringIn struct {
	Env             Env
	Clock           clock.Clock
	Pack            *trustpack.Pack
	Tenants         []string
	Pools           map[string]*pgxpool.Pool
	Keys            *grants.Keys
	Understanding   UnderstandingParts
	OwnershipReader ownershipport.ScopedReader
	Screen          ScreenDeps
	ListEntities    map[string]intakesources.ListSpec
	Begin           trustport.TxBeginner
	Facts           trustport.RecordFactSource
}

// trustWired is what the wiring built.
type trustWired struct {
	Inputs    trustInputs
	Decisions decisionParts
	Reasons   map[string][]string
}

func wireTrustInputs(in trustWiringIn) (trustWired, error) { return trustWired{}, nil }

func (w trustWired) fill(a TrustAdapters) TrustAdapters { return a }

// Unbuilt names, in the order of TrustAdapters, the adapters of the wiring that are still not built.
func (w trustWired) Unbuilt() []string { return nil }

func memoPackSource(src PackSource) PackSource { return src }

// needTrustLimits is the fixed text of the Clocks gap.
const needTrustLimits = "the Trust limits file (trust_limits.json: no loader on main)"

var errNotBuilt = errors.New("cmd/core: not built")

// planContextsFromRequest is plangraphs.PlanContexts over the fields of the intake's PlanRequest.
type planContextsFromRequest struct{}

var _ plangraphs.PlanContexts = planContextsFromRequest{}

func (planContextsFromRequest) PlanContext(context.Context, intake.PlanRequest) (plangraphs.PlanContext, error) {
	return plangraphs.PlanContext{}, errNotBuilt
}
