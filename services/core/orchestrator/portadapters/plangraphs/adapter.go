package plangraphs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Graph reads the PlanContext of the request through the Contexts port and plans with it (GraphWith). An error of the port is a dependency failure, never a refusal.
func (a Adapter) Graph(ctx context.Context, r intake.PlanRequest) (trustport.PlanGraph, error) {
	if a.Contexts == nil {
		return trustport.PlanGraph{}, trustport.ErrConfig
	}
	pc, err := a.Contexts.PlanContext(ctx, r)
	if err != nil {
		return trustport.PlanGraph{}, fmt.Errorf("plangraphs: read the plan context: %w", err)
	}
	return a.GraphWith(ctx, r, pc)
}

// GraphWith returns the RUNNABLE steps of the pack's action graph for the request's intent node, in the graph's order, each with its params built from the admitted intent's
// RESOLVED slots, the pack's own values, and the policy params, facts and evidence packet of pc, and from nothing else (INV-02, AGENT-09). Every step's TargetRef is the stored
// intent's target (plan.v1 has none; INV-18). A refusal wraps trustport.ErrPlanGraphInvalid, which the Planner maps to intake.ErrPlanInvalid; an error of a port is returned as a
// dependency failure and is never ErrPlanGraphInvalid. Every returned value is a copy.
func (a Adapter) GraphWith(ctx context.Context, r intake.PlanRequest, pc PlanContext) (trustport.PlanGraph, error) {
	if a.Pack == nil || a.Intents == nil {
		return trustport.PlanGraph{}, trustport.ErrConfig
	}
	in, err := a.Intents.Slots(ctx, r.TenantID, r.IntentID, r.IntentVersion)
	if err != nil {
		return trustport.PlanGraph{}, fmt.Errorf("plangraphs: read the admitted intent: %w", err)
	}
	if in.TenantID != r.TenantID || in.IntentID != r.IntentID || in.Version != r.IntentVersion || in.IntentNode != r.IntentNode || in.IntentHash != r.IntentHash {
		return trustport.PlanGraph{}, fmt.Errorf("%w: the stored intent is not the one requested", ErrWrongIntent)
	}
	pg, err := a.Pack.ActionGraph(ctx, r.IntentNode)
	switch {
	case errors.Is(err, ErrNoGraph):
		return trustport.PlanGraph{}, fmt.Errorf("%w: %w", trustport.ErrPlanGraphInvalid, err)
	case err != nil:
		return trustport.PlanGraph{}, fmt.Errorf("plangraphs: read the pack's action graph: %w", err)
	}
	if pg.IntentNode != r.IntentNode {
		return trustport.PlanGraph{}, fmt.Errorf("%w: the graph is for %q, not %q", ErrWrongIntent, pg.IntentNode, r.IntentNode)
	}
	if len(pg.Steps) == 0 {
		return trustport.PlanGraph{}, fmt.Errorf("%w: the graph has no step", trustport.ErrPlanGraphInvalid)
	}
	if in.TargetRef == "" {
		return trustport.PlanGraph{}, fmt.Errorf("%w: the admitted intent names no target", ErrTargetMismatch)
	}
	runnable, err := runnableSet(pg, pc.Runnable)
	if err != nil {
		return trustport.PlanGraph{}, err
	}
	out := trustport.PlanGraph{GraphVersion: pg.GraphVersion, Steps: make([]trustport.PlanGraphStep, 0, len(runnable))}
	for _, s := range pg.Steps {
		if !runnable[s.CapabilityID] {
			continue
		}
		if s.TargetRef != "" && s.TargetRef != in.TargetRef {
			return trustport.PlanGraph{}, fmt.Errorf("%w: step %q", ErrTargetMismatch, s.NodeID)
		}
		params, err := buildParams(s, in.Slots, pc)
		if err != nil {
			return trustport.PlanGraph{}, err
		}
		out.Steps = append(out.Steps, trustport.PlanGraphStep{NodeID: s.NodeID, CapabilityID: s.CapabilityID, TargetRef: in.TargetRef, Params: params,
			Preconditions: append([]string{}, s.Preconditions...), Postconditions: append([]string{}, s.Postconditions...)})
	}
	return out, nil
}

// runnableSet is the set of capability ids to plan. It must be non-empty and every id must be a capability of the graph: a runnable id the graph does not have is refused.
func runnableSet(pg PackGraph, ids []string) (map[string]bool, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: the request names none", ErrNoRunnable)
	}
	inGraph := make(map[string]bool, len(pg.Steps))
	for _, s := range pg.Steps {
		inGraph[s.CapabilityID] = true
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !inGraph[id] {
			return nil, fmt.Errorf("%w: %q", ErrNoRunnable, id)
		}
		set[id] = true
	}
	return set, nil
}

// buildParams builds the params of one step from its declarations. A declaration with an empty or repeated name, or with a kind outside the six, is refused.
func buildParams(s PackStep, slots map[string]SlotValue, pc PlanContext) (map[string]any, error) {
	params := make(map[string]any, len(s.Params))
	for _, p := range s.Params {
		if p.Name == "" {
			return nil, fmt.Errorf("%w: step %q has a param with no name", ErrParamSource, s.NodeID)
		}
		if _, dup := params[p.Name]; dup {
			return nil, fmt.Errorf("%w: step %q declares param %q twice", ErrParamSource, s.NodeID, p.Name)
		}
		v, err := paramValue(p, s.CapabilityID, slots, pc)
		if err != nil {
			return nil, fmt.Errorf("step %q param %q: %w", s.NodeID, p.Name, err)
		}
		params[p.Name] = v
	}
	return params, nil
}

func paramValue(p ParamSpec, capabilityID string, slots map[string]SlotValue, pc PlanContext) (any, error) {
	switch p.Kind {
	case KindConst:
		return copyData(p.Value)
	case KindSlot:
		return resolvedSlot(p.Slot, slots)
	case KindMap:
		return mapped(p, slots)
	case KindFact:
		return present(p.Ref, pc.Facts[p.Ref])
	case KindPolicyParam:
		return present(p.Ref, pc.PolicyParams[capabilityID][p.Ref])
	case KindEvidencePacket:
		return present("evidence packet", pc.EvidencePacket)
	}
	return nil, ErrParamSource
}

// present returns a run-time value, or ErrNotPlannable when it is absent or empty. An unnamed fact or policy param is a bad declaration.
func present(name, v string) (any, error) {
	if name == "" {
		return nil, ErrParamSource
	}
	if v == "" {
		return nil, fmt.Errorf("%w: %q", ErrNotPlannable, name)
	}
	return v, nil
}

// mapped looks the RESOLVED value of the slot up in the pack's map. A value that is not a string key of the map is refused, never passed through.
func mapped(p ParamSpec, slots map[string]SlotValue) (any, error) {
	if len(p.Map) == 0 {
		return nil, fmt.Errorf("%w: a map param declares no values", ErrParamSource)
	}
	for _, code := range p.Map {
		if code == "" {
			return nil, fmt.Errorf("%w: a map param declares an empty code", ErrParamSource)
		}
	}
	if p.Slot == "" {
		return nil, ErrParamSource
	}
	sv, have := slots[p.Slot]
	if !have || !sv.Resolved {
		return nil, ErrUnresolvedSlot
	}
	key, ok := sv.Value.(string)
	if !ok {
		return nil, ErrMapValue
	}
	code, found := p.Map[key]
	if !found {
		return nil, ErrMapValue
	}
	return code, nil
}

func resolvedSlot(name string, slots map[string]SlotValue) (any, error) {
	if name == "" {
		return nil, ErrParamSource
	}
	sv, ok := slots[name]
	if !ok || !sv.Resolved {
		return nil, ErrUnresolvedSlot
	}
	return copyData(sv.Value)
}

// copyData deep-copies plain data: strings, booleans, integers, and maps and slices of them. Floats (and a json.Number that is not an integer) are refused, because money is
// integer minor units; anything else, nil included, is refused, so a param is always plain data.
func copyData(v any) (any, error) {
	switch x := v.(type) {
	case string, bool, int, int32, int64:
		return x, nil
	case json.Number:
		if strings.ContainsAny(string(x), ".eE") {
			return nil, fmt.Errorf("%w: a number that is not an integer", ErrParamSource)
		}
		return x, nil
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			c, err := copyData(e)
			if err != nil {
				return nil, err
			}
			m[k] = c
		}
		return m, nil
	case []any:
		l := make([]any, 0, len(x))
		for _, e := range x {
			c, err := copyData(e)
			if err != nil {
				return nil, err
			}
			l = append(l, c)
		}
		return l, nil
	}
	return nil, fmt.Errorf("%w: a value that is not plain data", ErrParamSource)
}
