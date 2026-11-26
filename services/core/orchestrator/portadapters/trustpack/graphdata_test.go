package trustpack_test

// A684 (interim, relayed; the Runnable gap of the G1 critical path), A363, A551. REQ-AG-001; INV-14, INV-16. The expectation is read from the real fintech pack's graph files by code
// that does not use the package under test. SYNTHETIC.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

type gdNode struct {
	ID            string   `json:"node_id"`
	Kind          string   `json:"kind"`
	Capability    string   `json:"capability_id"`
	Preconditions []string `json:"preconditions"`
}

type gdEdge struct {
	Node      string  `json:"node_id"`
	DependsOn string  `json:"depends_on"`
	Condition *string `json:"condition"`
}

type gdGraph struct {
	ID    string   `json:"graph_id"`
	Roots []string `json:"roots"`
	Nodes []gdNode `json:"nodes"`
	Edges []gdEdge `json:"edges"`
}

func rawGraphData(t testing.TB) []gdGraph {
	t.Helper()
	var out []gdGraph
	for _, rel := range readRawPack(t).Graphs {
		var g gdGraph
		if err := jsonUnmarshal(readPackFile(t, rel), &g); err != nil {
			t.Fatal(err)
		}
		out = append(out, g)
	}
	if len(out) == 0 {
		t.Fatal("the real pack lists no graph")
	}
	return out
}

func expectedGraph(g gdGraph) trustpack.GraphData {
	var out trustpack.GraphData
	out.Roots = append(out.Roots, g.Roots...)
	for _, n := range g.Nodes {
		out.Nodes = append(out.Nodes, trustpack.GraphNode{NodeID: n.ID, Kind: n.Kind, CapabilityID: n.Capability, Preconditions: append([]string{}, n.Preconditions...)})
	}
	for _, e := range g.Edges {
		c := ""
		if e.Condition != nil {
			c = *e.Condition
		}
		out.Edges = append(out.Edges, trustpack.GraphEdge{NodeID: e.Node, DependsOn: e.DependsOn, Condition: c})
	}
	return out
}

// A551: for every graph of the real pack the accessor gives the graph's roots, nodes (with their kind, capability and preconditions) and edges (with their condition), in the file's order.
func TestActionGraphDataIsEveryGraphOfTheRealPackAsData(t *testing.T) {
	p := loadReal(t)
	for _, g := range rawGraphData(t) {
		got, err := p.ActionGraphData(g.ID)
		if err != nil {
			t.Fatalf("graph %q: %v", g.ID, err)
		}
		if want := expectedGraph(g); !reflect.DeepEqual(got, want) {
			t.Errorf("graph %q = %+v; want %+v", g.ID, got, want)
		}
	}
}

// The answer is a copy: changing it changes nothing of the pack.
func TestActionGraphDataIsACopy(t *testing.T) {
	p := loadReal(t)
	g := rawGraphData(t)[0]
	got, err := p.ActionGraphData(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Roots) == 0 || len(got.Nodes) == 0 {
		t.Fatalf("the accessor gave an empty graph: %+v", got)
	}
	got.Roots[0] = "CHANGED"
	got.Nodes[0].NodeID = "CHANGED"
	got.Nodes[0].Preconditions = append(got.Nodes[0].Preconditions, "CHANGED")
	if len(got.Edges) > 0 {
		got.Edges[0].Condition = "CHANGED"
	}
	again, err := p.ActionGraphData(g.ID)
	if err != nil || !reflect.DeepEqual(again, expectedGraph(g)) {
		t.Fatalf("changing the answer changed the pack: %+v, %v", again, err)
	}
}

// INV-16: nothing is answered from nothing. An empty graph ID and a graph the pack does not have are ErrGraph; a nil pack is ErrConfig.
func TestActionGraphDataRefusesWhatItCannotAnswer(t *testing.T) {
	p := loadReal(t)
	for _, id := range []string{"", rawGraphData(t)[0].ID + "_absent"} {
		if got, err := p.ActionGraphData(id); !errors.Is(err, trustpack.ErrGraph) || !reflect.DeepEqual(got, trustpack.GraphData{}) {
			t.Errorf("ActionGraphData(%q) = %+v, %v; want none and ErrGraph", id, got, err)
		}
	}
	var nilPack *trustpack.Pack
	if got, err := nilPack.ActionGraphData(rawGraphData(t)[0].ID); !errors.Is(err, trustpack.ErrConfig) || !reflect.DeepEqual(got, trustpack.GraphData{}) {
		t.Errorf("(*Pack)(nil).ActionGraphData = %+v, %v; want none and ErrConfig", got, err)
	}
}
