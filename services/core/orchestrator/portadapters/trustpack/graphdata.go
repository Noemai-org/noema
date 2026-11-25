package trustpack

import "github.com/Noemai-org/noema/services/core/internal/contracts"

// GraphNode is one node of an action graph as the pack file writes it, reduced to what deciding "runnable now" needs (REQ-AG-001).
type GraphNode struct {
	NodeID        string
	Kind          string
	CapabilityID  string
	Preconditions []string
}

// GraphEdge says NodeID depends on DependsOn; a non-empty Condition is one more predicate that must hold.
type GraphEdge struct {
	NodeID    string
	DependsOn string
	Condition string
}

// GraphData is an action graph as data: its roots, nodes and edges, in the pack file's order. The predicates are text: this package does not parse or evaluate them.
type GraphData struct {
	Roots []string
	Nodes []GraphNode
	Edges []GraphEdge
}

func readGraphData(g contracts.ActionGraphV2) GraphData {
	var out GraphData
	for _, r := range g.Roots {
		out.Roots = append(out.Roots, string(r))
	}
	for _, n := range g.Nodes {
		node := GraphNode{NodeID: string(n.NodeId), Kind: string(n.Kind), CapabilityID: string(n.CapabilityId), Preconditions: []string{}}
		for _, c := range n.Preconditions {
			node.Preconditions = append(node.Preconditions, string(c))
		}
		out.Nodes = append(out.Nodes, node)
	}
	for _, e := range g.Edges {
		edge := GraphEdge{NodeID: string(e.NodeId), DependsOn: string(e.DependsOn)}
		if e.Condition != nil {
			edge.Condition = *e.Condition
		}
		out.Edges = append(out.Edges, edge)
	}
	return out
}

// ActionGraphData is the pack's action graph `graphID` as data (A551, A684; read-only), read from the pack bytes LoadPack verified. The answer and every list in it are copies: the
// pack is never exposed and never changed. A nil or zero Pack is ErrConfig; an empty graph ID or a graph the pack does not have is ErrGraph (INV-16). It names no graph, node or
// capability (INV-14).
func (p *Pack) ActionGraphData(graphID string) (GraphData, error) {
	if !p.usable() {
		return GraphData{}, ErrConfig
	}
	g, ok := p.graphData[graphID]
	if graphID == "" || !ok {
		return GraphData{}, ErrGraph
	}
	out := GraphData{Roots: append([]string{}, g.Roots...)}
	for _, n := range g.Nodes {
		n.Preconditions = append([]string{}, n.Preconditions...)
		out.Nodes = append(out.Nodes, n)
	}
	out.Edges = append(out.Edges, g.Edges...)
	return out, nil
}
