package trustpack_test

// STM-01 composition, the Planner (A684 step 2, A551, A454, interim, relayed, not yet committed; REQ-STM-001, REQ-TRU-009; INV-14, INV-16, INV-02): ONE read-only accessor of the
// intent nodes' action graph, their slot declarations and the graph's params. The expectation is read from the real fintech pack by code that does not use the package under test.
// SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

type npSlot struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	ResourceKind *string `json:"resource_kind"`
}

type npNode struct {
	ID          string   `json:"node_id"`
	ActionGraph *string  `json:"action_graph"`
	Slots       []npSlot `json:"slots"`
}

type npParam struct {
	Name  string           `json:"name"`
	Slot  *string          `json:"slot"`
	Const *json.RawMessage `json:"const"`
	Map   *struct {
		From   string            `json:"from"`
		Values map[string]string `json:"values"`
	} `json:"map"`
	Fact           *string `json:"fact"`
	PolicyParam    *string `json:"policy_param"`
	EvidencePacket *bool   `json:"evidence_packet"`
}

type npGraph struct {
	ID      string `json:"graph_id"`
	Version string `json:"graph_version"`
	Nodes   []struct {
		ID             string    `json:"node_id"`
		Capability     string    `json:"capability_id"`
		Preconditions  []string  `json:"preconditions"`
		Postconditions []string  `json:"postconditions"`
		Params         []npParam `json:"params"`
	} `json:"nodes"`
}

func readNPNodes(t testing.TB) []npNode {
	t.Helper()
	var raw struct {
		Nodes []npNode `json:"intent_nodes"`
	}
	if err := json.Unmarshal(readPackFile(t, packFileName), &raw); err != nil {
		t.Fatal(err)
	}
	return raw.Nodes
}

func readNPGraph(t testing.TB, id string) (npGraph, bool) {
	t.Helper()
	for _, rel := range readRawPack(t).Graphs {
		var g npGraph
		if err := json.Unmarshal(readPackFile(t, rel), &g); err != nil {
			t.Fatal(err)
		}
		if g.ID == id {
			return g, true
		}
	}
	return npGraph{}, false
}

// expectedPlan is what the pack file says of one intent node, built without the package under test.
func expectedPlan(t testing.TB, n npNode, g npGraph) trustpack.NodePlan {
	t.Helper()
	want := trustpack.NodePlan{GraphID: g.ID, GraphVersion: g.Version}
	for _, s := range n.Slots {
		slot := trustpack.PlanSlot{Name: s.Name, Type: s.Type}
		if s.ResourceKind != nil {
			slot.ResourceKind = *s.ResourceKind
		}
		want.Slots = append(want.Slots, slot)
	}
	for _, gn := range g.Nodes {
		step := trustpack.PlanStep{NodeID: gn.ID, CapabilityID: gn.Capability, Preconditions: append([]string{}, gn.Preconditions...), Postconditions: append([]string{}, gn.Postconditions...)}
		for _, q := range gn.Params {
			p := trustpack.PlanParam{Name: q.Name}
			if q.Slot != nil {
				p.Slot = *q.Slot
			}
			if q.Const != nil {
				p.ConstJSON = string(*q.Const)
			}
			if q.Map != nil {
				p.HasMap, p.MapFrom, p.MapValues = true, q.Map.From, q.Map.Values
			}
			if q.Fact != nil {
				p.Fact = *q.Fact
			}
			if q.PolicyParam != nil {
				p.PolicyParam = *q.PolicyParam
			}
			p.EvidencePacket = q.EvidencePacket != nil && *q.EvidencePacket
			step.Params = append(step.Params, p)
		}
		want.Steps = append(want.Steps, step)
	}
	return want
}

// Every intent node that names an action graph has an entry with that graph's version, the node's own slot declarations (with the resource kind) and the graph's nodes with their
// params, in the files' order; a node that names none has no entry.
func TestNodePlansIsTheNodesGraphSlotsAndParamsAsThePackWritesThem(t *testing.T) {
	got, err := loadReal(t).NodePlans()
	if err != nil {
		t.Fatal(err)
	}
	with, without, params := 0, 0, 0
	for _, n := range readNPNodes(t) {
		if n.ActionGraph == nil {
			without++
			if _, ok := got[n.ID]; ok {
				t.Errorf("NodePlans has an entry for %q, which names no graph", n.ID)
			}
			continue
		}
		g, ok := readNPGraph(t, *n.ActionGraph)
		if !ok {
			t.Fatalf("the pack's node %q names a graph its files lack", n.ID)
		}
		with++
		want := expectedPlan(t, n, g)
		for _, s := range want.Steps {
			params += len(s.Params)
		}
		if !reflect.DeepEqual(got[n.ID], want) {
			t.Fatalf("NodePlans[%q] = %+v; want %+v", n.ID, got[n.ID], want)
		}
	}
	if with == 0 || without == 0 || params == 0 {
		t.Fatalf("the pack must give a node with a graph with params and a node without one (%d, %d, %d): the test is vacuous otherwise", with, without, params)
	}
}

// The answer is a copy: changing it changes nothing the pack holds (the pack is never exposed).
func TestNodePlansReturnsACopy(t *testing.T) {
	p := loadReal(t)
	first, err := p.NodePlans()
	if err != nil {
		t.Fatal(err)
	}
	for id, np := range first {
		np.Slots[0].Name = "MUTATED"
		for i := range np.Steps {
			if len(np.Steps[i].Preconditions) > 0 {
				np.Steps[i].Preconditions[0] = "MUTATED"
			}
			np.Steps[i].Preconditions = append(np.Steps[i].Preconditions, "MUTATED")
			if len(np.Steps[i].Postconditions) > 0 {
				np.Steps[i].Postconditions[0] = "MUTATED"
			}
			for j := range np.Steps[i].Params {
				for k := range np.Steps[i].Params[j].MapValues {
					np.Steps[i].Params[j].MapValues[k] = "MUTATED"
				}
			}
			np.Steps[i].Params = append(np.Steps[i].Params, trustpack.PlanParam{Name: "MUTATED"})
		}
		first[id] = np
	}
	second, err := p.NodePlans()
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := loadReal(t).NodePlans()
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) == 0 || !reflect.DeepEqual(second, fresh) {
		t.Fatal("changing the answer changed what the pack holds")
	}
}

// A node whose action graph the pack does not have gets no entry: the Planner then has no graph for it and refuses (INV-16).
func TestANodeWhoseGraphThePackLacksHasNoPlan(t *testing.T) {
	m := files(t)
	var id string
	m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
		for _, n := range p["intent_nodes"].([]any) {
			node := n.(map[string]any)
			if node["action_graph"] != nil {
				id = node["node_id"].(string)
				node["action_graph"] = "fin.graph.synthetic_absent"
			}
		}
	})
	got, err := mustLoadFiles(t, m).NodePlans()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[id]; id == "" || ok {
		t.Fatalf("NodePlans[%q] = %+v; want no entry for a node whose graph the pack lacks", id, got[id])
	}
}

// INV-16: a nil or zero Pack answers nothing, and a pack that fails to load is no Pack, so it yields none.
func TestAPackThatFailsVerificationYieldsNoNodePlans(t *testing.T) {
	var nilPack *trustpack.Pack
	for name, p := range map[string]*trustpack.Pack{"nil": nilPack, "zero": {}} {
		if got, err := p.NodePlans(); !errors.Is(err, trustpack.ErrConfig) || got != nil {
			t.Fatalf("%s Pack: NodePlans = %v, %v; want nil and ErrConfig", name, got, err)
		}
	}
	m := files(t)
	m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
		ns := p["intent_nodes"].([]any)
		p["intent_nodes"] = append(ns, ns[0])
	})
	p, err := loadFiles(t, m)
	if !errors.Is(err, trustpack.ErrPack) || p != nil {
		t.Fatalf("LoadPack(a repeated intent node) = %v, %v; want a nil Pack and ErrPack", p, err)
	}
	if got, err := p.NodePlans(); !errors.Is(err, trustpack.ErrConfig) || got != nil {
		t.Fatalf("the Pack of a failed load: NodePlans = %v, %v; want nil and ErrConfig", got, err)
	}
}
