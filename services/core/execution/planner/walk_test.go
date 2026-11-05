package planner_test

// PLN-01 slice 2: the plan is chosen by walking the Action Graph, and every emitted plan follows the declared edges.
// REQ-PLN-001, REQ-PLN-002, REQ-AG-001, INV-18. Everything is SYNTHETIC; the graphs are the repository's own fintech graphs.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/planner"
	"github.com/Noemai-org/noema/services/core/execution/registry"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

func loadGraph(t testing.TB, name string) contracts.ActionGraphV2 {
	t.Helper()
	b, err := os.ReadFile("../../../../packs/fintech/graph/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var g contracts.ActionGraphV2
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// follows checks the property itself: the plan holds every node of the graph exactly once, and no node comes before a node it depends on.
func follows(g contracts.ActionGraphV2, p planner.Plan) error {
	pos := map[string]int{}
	for i, s := range p.Document().Steps {
		if _, dup := pos[string(s.NodeId)]; dup {
			return fmt.Errorf("node %s appears twice", s.NodeId)
		}
		if int(s.Step) != i {
			return fmt.Errorf("step %d is numbered %d", i, s.Step)
		}
		pos[string(s.NodeId)] = i
	}
	if len(pos) != len(g.Nodes) {
		return fmt.Errorf("the plan has %d steps for %d nodes", len(pos), len(g.Nodes))
	}
	for _, n := range g.Nodes {
		if _, ok := pos[string(n.NodeId)]; !ok {
			return fmt.Errorf("node %s is missing from the plan", n.NodeId)
		}
	}
	for _, e := range g.Edges {
		if pos[string(e.DependsOn)] >= pos[string(e.NodeId)] {
			return fmt.Errorf("%s is planned at %d, not after %s at %d, although the graph says it depends on it", e.NodeId, pos[string(e.NodeId)], e.DependsOn, pos[string(e.DependsOn)])
		}
	}
	return nil
}

func TestEveryFintechGraphWalksToAPlanThatFollowsItsEdges(t *testing.T) {
	for _, name := range []string{"g1_investigate.json", "g2_escalate.json", "g3_complaint.json"} {
		g := loadGraph(t, name)
		p, err := planner.Walk(reg(t), g, string(g.GraphId), nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := follows(g, p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		h1, _ := p.Hash()
		p2, _ := planner.Walk(reg(t), g, string(g.GraphId), nil)
		if h2, _ := p2.Hash(); h1 != h2 {
			t.Fatalf("%s: the same graph gave two plan hashes", name)
		}
	}
}

func TestTheWalkCopiesTheNodesConditionsIntoTheSteps(t *testing.T) {
	g := loadGraph(t, "g2_escalate.json")
	p, err := planner.Walk(reg(t), g, string(g.GraphId), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]contracts.ActionGraphV2NodesItem{}
	for _, n := range g.Nodes {
		want[string(n.NodeId)] = n
	}
	for _, s := range p.Document().Steps {
		n := want[string(s.NodeId)]
		if string(s.CapabilityId) != string(n.CapabilityId) || len(s.Preconditions) != len(n.Preconditions) || len(s.ExpectedPostconditions) != len(n.Postconditions) {
			t.Fatalf("step %s does not carry its node's capability and conditions: %+v", s.NodeId, s)
		}
	}
}

// refusals: a graph the walk cannot follow produces no plan at all.
func TestTheWalkRefusesAGraphItCannotFollow(t *testing.T) {
	base := func() contracts.ActionGraphV2 { return loadGraph(t, "g1_investigate.json") }
	cond := "subject_resource.status == 'RESOLVED'"
	cases := []struct {
		name string
		edit func(g *contracts.ActionGraphV2)
	}{
		{"a cycle", func(g *contracts.ActionGraphV2) {
			e := g.Edges[0]
			g.Edges = append(g.Edges, contracts.ActionGraphV2EdgesItem{NodeId: e.DependsOn, DependsOn: e.NodeId})
		}},
		{"an edge from an unknown node", func(g *contracts.ActionGraphV2) {
			g.Edges = append(g.Edges, contracts.ActionGraphV2EdgesItem{NodeId: "ghost", DependsOn: g.Nodes[0].NodeId})
		}},
		{"an edge to an unknown node", func(g *contracts.ActionGraphV2) {
			g.Edges = append(g.Edges, contracts.ActionGraphV2EdgesItem{NodeId: g.Nodes[0].NodeId, DependsOn: "ghost"})
		}},
		{"a node that depends on itself", func(g *contracts.ActionGraphV2) {
			g.Edges = append(g.Edges, contracts.ActionGraphV2EdgesItem{NodeId: g.Nodes[0].NodeId, DependsOn: g.Nodes[0].NodeId})
		}},
		{"a conditional edge (conditions are not evaluated here, so the walk cannot know the path)", func(g *contracts.ActionGraphV2) {
			g.Edges[0].Condition = &cond
		}},
		{"a duplicate node", func(g *contracts.ActionGraphV2) { g.Nodes = append(g.Nodes, g.Nodes[0]) }},
		{"no nodes", func(g *contracts.ActionGraphV2) { g.Nodes, g.Edges, g.Roots = nil, nil, nil }},
		{"an unregistered capability", func(g *contracts.ActionGraphV2) { g.Nodes[0].CapabilityId = "fin.cap.not_registered" }},
		{"a root that has an incoming edge", func(g *contracts.ActionGraphV2) {
			g.Edges = append(g.Edges, contracts.ActionGraphV2EdgesItem{NodeId: g.Roots[0], DependsOn: g.Nodes[len(g.Nodes)-1].NodeId})
		}},
	}
	for _, c := range cases {
		g := base()
		c.edit(&g)
		if p, err := planner.Walk(reg(t), g, string(g.GraphId), nil); err == nil {
			t.Errorf("%s: a plan was built: %+v", c.name, p.Document())
		}
	}
	if _, err := planner.Walk(nil, base(), "fin.graph.investigate", nil); err == nil {
		t.Error("no registry: a plan was built")
	}
	if _, err := planner.Walk(reg(t), base(), "", nil); err == nil {
		t.Error("no intent node: a plan was built")
	}
}

func TestAProhibitedCapabilityIsNeverPlannedFromAGraph(t *testing.T) {
	r := reg(t)
	prohibited := r.Prohibited()
	if len(prohibited) == 0 {
		t.Skip("the pack has no prohibited capability")
	}
	g := loadGraph(t, "g1_investigate.json")
	g.Nodes[0].CapabilityId = contracts.CommonV1PackId(prohibited[0])
	_, err := planner.Walk(r, g, string(g.GraphId), nil)
	var re *registry.Error
	if !errors.As(err, &re) || re.Code != registry.CodeProhibited {
		t.Fatalf("want CAPABILITY_PROHIBITED, got %v", err)
	}
}

// The property: for random acyclic graphs whose nodes and edges are listed in a random order, the plan follows every edge, holds every
// node once, and is the same plan (the same hash) however the document is ordered.
func TestEveryPlanFromARandomGraphFollowsItsEdges(t *testing.T) {
	ids := allowed(t, reg(t))
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 12).Draw(t, "nodes")
		g := contracts.ActionGraphV2{GraphId: "fin.graph.random", GraphVersion: "1.0.0", AppliesWhen: "true"}
		for i := 0; i < n; i++ {
			c := rapid.SampledFrom(ids).Draw(t, "capability")
			g.Nodes = append(g.Nodes, contracts.ActionGraphV2NodesItem{NodeId: contracts.CommonV1SlotName(fmt.Sprintf("n%02d", i)), Kind: contracts.ActionGraphV2NodesItemKindREAD,
				CapabilityId: contracts.CommonV1PackId(c), Preconditions: []contracts.CommonV1Predicate{}, Postconditions: []contracts.CommonV1Predicate{"true"},
				OnUnknown: contracts.ActionGraphV2RecoveryActionESCALATE, OnFailure: contracts.ActionGraphV2RecoveryActionRETRYREAD})
		}
		hasIn := map[int]bool{}
		for j := 1; j < n; j++ {
			for i := 0; i < j; i++ {
				if rapid.Bool().Draw(t, fmt.Sprintf("edge %d<-%d", j, i)) {
					g.Edges = append(g.Edges, contracts.ActionGraphV2EdgesItem{NodeId: g.Nodes[j].NodeId, DependsOn: g.Nodes[i].NodeId})
					hasIn[j] = true
				}
			}
		}
		for i := range g.Nodes {
			if !hasIn[i] {
				g.Roots = append(g.Roots, g.Nodes[i].NodeId)
			}
		}
		p, err := planner.Walk(reg(t), g, "fin.graph.random", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := follows(g, p); err != nil {
			t.Fatal(err)
		}
		h, _ := p.Hash()
		shuffled := g
		shuffled.Nodes = append([]contracts.ActionGraphV2NodesItem(nil), g.Nodes...)
		shuffled.Edges = append([]contracts.ActionGraphV2EdgesItem(nil), g.Edges...)
		nk := rapid.SliceOfN(rapid.IntRange(0, 1000), len(shuffled.Nodes), len(shuffled.Nodes)).Draw(t, "node order keys")
		ek := rapid.SliceOfN(rapid.IntRange(0, 1000), len(shuffled.Edges), len(shuffled.Edges)).Draw(t, "edge order keys")
		sort.Stable(byKey{nk, func(i, j int) { shuffled.Nodes[i], shuffled.Nodes[j] = shuffled.Nodes[j], shuffled.Nodes[i] }})
		sort.Stable(byKey{ek, func(i, j int) { shuffled.Edges[i], shuffled.Edges[j] = shuffled.Edges[j], shuffled.Edges[i] }})
		q, err := planner.Walk(reg(t), shuffled, "fin.graph.random", nil)
		if err != nil {
			t.Fatal(err)
		}
		if h2, _ := q.Hash(); h != h2 {
			t.Fatal("the plan depends on the order in which the document lists its nodes and edges")
		}
	})
}

// byKey sorts a slice by an integer key per element, swapping the keys with the elements.
type byKey struct {
	keys []int
	swap func(i, j int)
}

func (b byKey) Len() int           { return len(b.keys) }
func (b byKey) Less(i, j int) bool { return b.keys[i] < b.keys[j] }
func (b byKey) Swap(i, j int)      { b.keys[i], b.keys[j] = b.keys[j], b.keys[i]; b.swap(i, j) }
