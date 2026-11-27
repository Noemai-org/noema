package trustpack

import "github.com/Noemai-org/noema/services/core/internal/contracts"

// PlanSlot is one slot declaration of an intent node, as the pack file writes it: the node's slot is the single authority of a slot's type and resource kind (A454, interim, relayed).
// ResourceKind is "" when the slot declares none.
type PlanSlot struct {
	Name         string
	Type         string
	ResourceKind string
}

// PlanParam is one declared param of an action graph node, as the pack file writes it. A member the file does not write is its zero value; the package does not decide which source
// the param has (a param with none, or with more than one, is for the reader to refuse). ConstJSON is the JSON text of the constant. HasMap is true when the file writes a map param,
// whose slot is MapFrom and whose values are MapValues.
type PlanParam struct {
	Name           string
	Slot           string
	ConstJSON      string
	HasMap         bool
	MapFrom        string
	MapValues      map[string]string
	Fact           string
	PolicyParam    string
	EvidencePacket bool
}

// PlanStep is one node of an action graph with what the plan needs of it, in the graph file's order.
type PlanStep struct {
	NodeID         string
	CapabilityID   string
	Preconditions  []string
	Postconditions []string
	Params         []PlanParam
}

// NodePlan is the action graph an intent node names, with the node's slot declarations: what the Planner reads (A551, A684).
type NodePlan struct {
	GraphID      string
	GraphVersion string
	Slots        []PlanSlot
	Steps        []PlanStep
}

type nodeHead struct {
	graphID string
	slots   []PlanSlot
}

type planGraph struct {
	version string
	steps   []PlanStep
}

func readNodeHead(n contracts.IntentNodeV1) nodeHead {
	var h nodeHead
	if n.ActionGraph != nil {
		h.graphID = string(*n.ActionGraph)
	}
	for _, s := range n.Slots {
		slot := PlanSlot{Name: string(s.Name), Type: string(s.Type)}
		if s.ResourceKind != nil {
			slot.ResourceKind = string(*s.ResourceKind)
		}
		h.slots = append(h.slots, slot)
	}
	return h
}

func readPlanGraph(g contracts.ActionGraphV2) planGraph {
	out := planGraph{version: string(g.GraphVersion)}
	for _, n := range g.Nodes {
		step := PlanStep{NodeID: string(n.NodeId), CapabilityID: string(n.CapabilityId), Preconditions: []string{}, Postconditions: []string{}}
		for _, c := range n.Preconditions {
			step.Preconditions = append(step.Preconditions, string(c))
		}
		for _, c := range n.Postconditions {
			step.Postconditions = append(step.Postconditions, string(c))
		}
		if n.Params != nil {
			for _, q := range *n.Params {
				step.Params = append(step.Params, readPlanParam(q))
			}
		}
		out.steps = append(out.steps, step)
	}
	return out
}

func readPlanParam(q contracts.ActionGraphV2NodesItemParamsItem) PlanParam {
	p := PlanParam{Name: string(q.Name), ConstJSON: string(q.Const)}
	if q.Slot != nil {
		p.Slot = string(*q.Slot)
	}
	if q.Map != nil {
		p.HasMap, p.MapFrom, p.MapValues = true, string(q.Map.From), make(map[string]string, len(q.Map.Values))
		for k, v := range q.Map.Values {
			p.MapValues[k] = v
		}
	}
	if q.Fact != nil {
		p.Fact = string(*q.Fact)
	}
	if q.PolicyParam != nil {
		p.PolicyParam = string(*q.PolicyParam)
	}
	p.EvidencePacket = q.EvidencePacket != nil && *q.EvidencePacket
	return p
}

// NodePlans is, for every intent node whose action graph the pack names and has (A551, A684; read-only), that graph's version, its nodes with their params and the node's slot
// declarations, read from the pack bytes LoadPack verified. The map and every list and map in it are copies: the pack is never exposed and never changed. A node that names no graph,
// or one the pack does not have, has no entry. A nil or zero Pack, and a pack that did not load (it is no Pack), give none and ErrConfig (INV-16). It names no node, graph or slot
// (INV-14).
func (p *Pack) NodePlans() (map[string]NodePlan, error) {
	if !p.usable() {
		return nil, ErrConfig
	}
	out := make(map[string]NodePlan, len(p.nodeHeads))
	for id, h := range p.nodeHeads {
		g, ok := p.planGraphs[h.graphID]
		if h.graphID == "" || !ok {
			continue
		}
		np := NodePlan{GraphID: h.graphID, GraphVersion: g.version, Slots: append([]PlanSlot(nil), h.slots...)}
		for _, s := range g.steps {
			c := PlanStep{NodeID: s.NodeID, CapabilityID: s.CapabilityID, Preconditions: append([]string{}, s.Preconditions...), Postconditions: append([]string{}, s.Postconditions...)}
			for _, q := range s.Params {
				if q.MapValues != nil {
					m := make(map[string]string, len(q.MapValues))
					for k, v := range q.MapValues {
						m[k] = v
					}
					q.MapValues = m
				}
				c.Params = append(c.Params, q)
			}
			np.Steps = append(np.Steps, c)
		}
		out[id] = np
	}
	return out, nil
}
