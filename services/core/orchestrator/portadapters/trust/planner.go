package trustport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/execution/planner"
	"github.com/Noemai-org/noema/services/core/execution/registry"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// ErrPlanGraphInvalid is what a PlanGraphs implementation returns for an intent node whose action graph cannot give a plan (unknown node, a cycle, a missing step).
var ErrPlanGraphInvalid = errors.New("trustport: the action graph gives no plan for the intent node")

// PlanGraphStep is one step of an action graph, as data.
type PlanGraphStep struct {
	NodeID         string
	CapabilityID   string
	TargetRef      string
	Params         map[string]any
	Preconditions  []string
	Postconditions []string
}

// PlanGraph is the action graph of one intent node, in plan order, with its version.
type PlanGraph struct {
	GraphVersion string
	Steps        []PlanGraphStep
}

// PlanGraphs reads the pack's action graph for the intent node of a request. The composition fills it.
type PlanGraphs interface {
	Graph(ctx context.Context, r intake.PlanRequest) (PlanGraph, error)
}

// BuildFunc builds a plan; planner.New is the default.
type BuildFunc func(reg *registry.Registry, graphVersion, intentNode string, specs []planner.StepSpec) (planner.Plan, error)

// PlannerAdapter implements intake.Planner over execution/planner.
type PlannerAdapter struct {
	Registry *registry.Registry
	Graphs   PlanGraphs
	Build    BuildFunc
}

var _ intake.Planner = PlannerAdapter{}

// Plan is not built yet.
func (p PlannerAdapter) Plan(context.Context, intake.PlanRequest) (intake.Plan, error) {
	return intake.Plan{}, nil
}
