package main

import (
	"context"
	"errors"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/plangraphs"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

// The stub of the red commit: every part answers "not built".

const (
	needPlanTextBound   = "the Planner's slot-text bound (plangraphs.Adapter.MaxTextBytes: no ruling or setting gives a value)"
	needAdmittedIntents = "the admitted-intent store adapter (Intents)"
	needSlotDocuments   = "the stored intent document reader (the tenant's Trust login and its read bound)"
)

var errPlannerNotBuilt = errors.New("cmd/core: the Planner parts are not built")

type slotDocuments interface {
	Document(ctx context.Context, tenantID, intentID string, version int) (intentstore.Admitted, error)
}

type plannerInputs struct {
	Pack         *trustpack.Pack
	Intents      intake.Intents
	Docs         slotDocuments
	MaxTextBytes int
}

func buildPlanner(plannerInputs) (intake.Planner, []string, error) {
	return nil, nil, errPlannerNotBuilt
}

type packPlanGraphs struct{ plans map[string]trustpack.NodePlan }

var _ plangraphs.Pack = packPlanGraphs{}

func (packPlanGraphs) ActionGraph(context.Context, string) (plangraphs.PackGraph, error) {
	return plangraphs.PackGraph{}, errPlannerNotBuilt
}

type storedSlots struct {
	Intents intake.Intents
	Docs    slotDocuments
}

var _ plangraphs.Intents = storedSlots{}

func (storedSlots) Slots(context.Context, string, string, int) (plangraphs.IntentSlots, error) {
	return plangraphs.IntentSlots{}, errPlannerNotBuilt
}

type poolDocuments struct {
	pools   trustport.TenantPools
	timeout time.Duration
}

var _ slotDocuments = poolDocuments{}

func newPoolDocuments(trustport.TenantPools, time.Duration) (slotDocuments, error) {
	return nil, errPlannerNotBuilt
}

func (poolDocuments) Document(context.Context, string, string, int) (intentstore.Admitted, error) {
	return intentstore.Admitted{}, errPlannerNotBuilt
}
