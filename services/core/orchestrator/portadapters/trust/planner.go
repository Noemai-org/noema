package trustport

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/execution/planner"
	"github.com/Noemai-org/noema/services/core/execution/registry"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// readSchemaVersion is the plan.v1 version the planner hashes under.
const readSchemaVersion = "1.0"

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

// Plan builds the plan for the request. A write gets the steps of the intent node's action graph, in order, each with the hash of its params; a read gets no step and the
// hash of the empty plan (ruling A205 Q3, relayed, not yet committed). A refusal of the graph, the registry or the planner is intake.ErrPlanInvalid; every other error is
// returned as a dependency failure and is never ErrPlanInvalid.
func (p PlannerAdapter) Plan(ctx context.Context, r intake.PlanRequest) (intake.Plan, error) {
	if p.Registry == nil || p.Graphs == nil {
		return intake.Plan{}, ErrConfig
	}
	g, err := p.Graphs.Graph(ctx, r)
	switch {
	case errors.Is(err, ErrPlanGraphInvalid):
		return intake.Plan{}, fmt.Errorf("%w: %w", intake.ErrPlanInvalid, err)
	case err != nil:
		return intake.Plan{}, fmt.Errorf("trustport: read the action graph: %w", err)
	}
	if !r.WriteRequired {
		return p.readPlan(g, r)
	}
	if err := noRepeatedNode(g.Steps); err != nil {
		return intake.Plan{}, err
	}
	specs := make([]planner.StepSpec, 0, len(g.Steps))
	for _, s := range g.Steps {
		specs = append(specs, planner.StepSpec{NodeID: s.NodeID, CapabilityID: s.CapabilityID, Params: s.Params, Preconditions: s.Preconditions, Postconditions: s.Postconditions})
	}
	build := p.Build
	if build == nil {
		build = planner.New
	}
	built, err := build(p.Registry, g.GraphVersion, r.IntentNode, specs)
	if err != nil {
		return intake.Plan{}, planError(err)
	}
	hash, err := built.Hash()
	if err != nil {
		return intake.Plan{}, planError(err)
	}
	doc := built.Document()
	if len(doc.Steps) != len(g.Steps) {
		return intake.Plan{}, fmt.Errorf("%w: the plan has %d steps for %d in the graph", intake.ErrPlanInvalid, len(doc.Steps), len(g.Steps))
	}
	out := intake.Plan{Hash: hash, Steps: make([]intake.PlanStep, 0, len(doc.Steps))}
	for i, s := range doc.Steps {
		ph, err := crypto.HashOf(s.Params)
		if err != nil {
			return intake.Plan{}, fmt.Errorf("%w: hash the params of step %d: %v", intake.ErrPlanInvalid, i, err)
		}
		out.Steps = append(out.Steps, intake.PlanStep{CapabilityID: string(s.CapabilityId), TargetRef: g.Steps[i].TargetRef, ParamsHash: ph})
	}
	return out, nil
}

// readPlan is the plan of a read: the plan.v1 document with no step, hashed the way the planner hashes a plan. It still needs a graph version and an intent node.
func (p PlannerAdapter) readPlan(g PlanGraph, r intake.PlanRequest) (intake.Plan, error) {
	if g.GraphVersion == "" || r.IntentNode == "" {
		return intake.Plan{}, fmt.Errorf("%w: a plan needs a graph version and an intent node", intake.ErrPlanInvalid)
	}
	doc := contracts.PlanV1{SchemaVersion: readSchemaVersion, GraphVersion: contracts.CommonV1Semver(g.GraphVersion), IntentNode: contracts.CommonV1PackId(r.IntentNode),
		Steps: []contracts.PlanV1StepsItem{}}
	h, err := crypto.HashOf(doc)
	if err != nil {
		return intake.Plan{}, fmt.Errorf("%w: hash the empty plan: %v", intake.ErrPlanInvalid, err)
	}
	return intake.Plan{Hash: h, Steps: []intake.PlanStep{}}, nil
}

// noRepeatedNode refuses a step list that visits a node twice: a linear plan cannot hold a cycle, so a repeat is one (safer reading).
func noRepeatedNode(steps []PlanGraphStep) error {
	seen := make(map[string]bool, len(steps))
	for _, s := range steps {
		if seen[s.NodeID] {
			return fmt.Errorf("%w: node %q repeats, the graph has a cycle", intake.ErrPlanInvalid, s.NodeID)
		}
		seen[s.NodeID] = true
	}
	return nil
}

// planError maps the planner's own refusals (a *registry.Error with one of the three codes it raises) to ErrPlanInvalid; any other error is a dependency failure.
func planError(err error) error {
	var re *registry.Error
	if errors.As(err, &re) {
		switch re.Code {
		case registry.CodeNotRegistered, registry.CodeProhibited, registry.CodeIncomplete:
			return fmt.Errorf("%w: %w", intake.ErrPlanInvalid, err)
		}
	}
	return fmt.Errorf("trustport: build the plan: %w", err)
}
