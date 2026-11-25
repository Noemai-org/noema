package trustpack

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

// GraphData is an action graph as data: its roots, nodes and edges, in the pack file's order.
type GraphData struct {
	Roots []string
	Nodes []GraphNode
	Edges []GraphEdge
}

// ActionGraphData is the pack's action graph `graphID` as data (A551, A684; read-only). Stub: the implementation follows the red.
func (p *Pack) ActionGraphData(graphID string) (GraphData, error) {
	return GraphData{}, nil
}
