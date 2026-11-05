package planner

import (
	"fmt"
	"sort"

	"github.com/Noemai-org/noema/services/core/internal/contracts"

	"github.com/Noemai-org/noema/services/core/execution/registry"
)

// Walk chooses a plan by walking an Action Graph (REQ-PLN-001, REQ-AG-001): every node of the graph becomes one step, and a step comes only
// after every node it depends on. Among the nodes that are ready, the smallest node ID goes first, so the plan depends on the graph and
// not on the order in which the document lists its nodes and edges (INV-21). The capability of every node must resolve in the registry
// (INV-18), so a graph that names an unregistered or prohibited capability produces no plan.
//
// The walk refuses, and builds nothing, for a graph it cannot follow with certainty: no nodes, a duplicate node ID, an edge that names an
// unknown node or joins a node to itself, a cycle, a root that has an incoming edge, and a conditional edge (an edge condition is a
// predicate over facts this package does not hold, so which path it opens cannot be known here; the planner never guesses, fail closed).
//
// params gives the parameters of a step by node ID; a node without an entry has none. The preconditions and postconditions of a step are
// the node's own.
func Walk(reg *registry.Registry, g contracts.ActionGraphV2, intentNode string, params map[string]map[string]any) (Plan, error) {
	if reg == nil {
		return Plan{}, refuse("no registry")
	}
	if len(g.Nodes) == 0 {
		return Plan{}, refuse("the graph has no nodes")
	}
	nodes := make(map[string]contracts.ActionGraphV2NodesItem, len(g.Nodes))
	for _, n := range g.Nodes {
		id := string(n.NodeId)
		if _, dup := nodes[id]; dup {
			return Plan{}, refuse("node %q appears twice", id)
		}
		nodes[id] = n
	}
	waiting := make(map[string]int, len(nodes))         // how many dependencies of a node are not planned yet
	dependents := make(map[string][]string, len(nodes)) // the nodes that wait for a node
	seen := make(map[edgeKey]bool, len(g.Edges))
	for _, e := range g.Edges {
		from, on := string(e.NodeId), string(e.DependsOn)
		if _, ok := nodes[from]; !ok {
			return Plan{}, refuse("an edge starts at %q, which is not a node", from)
		}
		if _, ok := nodes[on]; !ok {
			return Plan{}, refuse("an edge depends on %q, which is not a node", on)
		}
		if from == on {
			return Plan{}, refuse("node %q depends on itself", from)
		}
		if e.Condition != nil {
			return Plan{}, refuse("the edge from %q to %q is conditional; the planner does not choose between paths", on, from)
		}
		if seen[edgeKey{from, on}] {
			continue // a repeated edge says nothing new
		}
		seen[edgeKey{from, on}] = true
		waiting[from]++
		dependents[on] = append(dependents[on], from)
	}
	// The declared roots must be exactly the nodes that depend on nothing: a root that is not a node or has an incoming edge, a node with no
	// dependency that the graph does not declare a root, and a graph with no roots are all refused, so a plan never starts from an
	// undeclared entry point (REQ-AG-001).
	declared := make(map[string]bool, len(g.Roots))
	for _, r := range g.Roots {
		id := string(r)
		if _, ok := nodes[id]; !ok {
			return Plan{}, refuse("root %q is not a node", id)
		}
		if waiting[id] > 0 {
			return Plan{}, refuse("root %q has an incoming edge", id)
		}
		declared[id] = true
	}
	var ready []string
	for id := range nodes {
		if waiting[id] == 0 {
			if !declared[id] {
				return Plan{}, refuse("node %q depends on nothing but is not a declared root", id)
			}
			ready = append(ready, id)
		}
	}
	if len(ready) == 0 {
		return Plan{}, refuse("the graph has no root")
	}
	var specs []StepSpec
	for len(ready) > 0 {
		sort.Strings(ready)
		id := ready[0]
		ready = ready[1:]
		n := nodes[id]
		specs = append(specs, StepSpec{NodeID: id, CapabilityID: string(n.CapabilityId), Params: params[id], Preconditions: predicateStrings(n.Preconditions), Postconditions: predicateStrings(n.Postconditions)})
		for _, d := range dependents[id] {
			if waiting[d]--; waiting[d] == 0 {
				ready = append(ready, d)
			}
		}
	}
	if len(specs) != len(nodes) {
		return Plan{}, refuse("the graph has a cycle: %d of %d nodes can be planned", len(specs), len(nodes))
	}
	p, err := New(reg, string(g.GraphVersion), intentNode, specs)
	if err != nil {
		return Plan{}, fmt.Errorf("%w (graph %s)", err, g.GraphId)
	}
	return p, nil
}

// edgeKey names an edge by the node that waits and the node it waits for.
type edgeKey struct{ from, on string }

func predicateStrings(in []contracts.CommonV1Predicate) []string {
	out := make([]string, len(in))
	for i, p := range in {
		out[i] = string(p)
	}
	return out
}
