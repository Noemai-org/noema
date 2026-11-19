package plangraphs

import (
	"context"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Graph reads the PlanContext of the request and plans with it. RED stub: behaviour not yet built.
func (a Adapter) Graph(ctx context.Context, r intake.PlanRequest) (trustport.PlanGraph, error) {
	return trustport.PlanGraph{}, trustport.ErrConfig
}

// GraphWith plans the runnable steps of the pack's graph with the given context. RED stub: behaviour not yet built.
func (a Adapter) GraphWith(ctx context.Context, r intake.PlanRequest, pc PlanContext) (trustport.PlanGraph, error) {
	return trustport.PlanGraph{}, trustport.ErrConfig
}
