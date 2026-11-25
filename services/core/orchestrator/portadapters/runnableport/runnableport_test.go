package runnableport_test

// A684 (interim, relayed; the Runnable gap of the G1 critical path), A363, A511, A531. REQ-AG-001, REQ-TRU-004; INV-01, INV-03, INV-14, INV-16. The graphs are the real fintech pack's
// (read by the adapter under test and, independently, by the test); the facts are SYNTHETIC. The seams (Graphs, FactsSource) are consumer-side ports the composition fills.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/runnable"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/runnableport"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const (
	tenant   = "ten_synthetic_1"
	packFile = "fintech_v1.json"
	node     = "node_synthetic"
)

func packsDir() string { return filepath.Join("..", "..", "..", "..", "..", "packs") }

func tctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func loadPack(t testing.TB) *trustpack.Pack {
	t.Helper()
	p, err := trustpack.LoadPack(os.DirFS(packsDir()), packFile)
	if err != nil {
		t.Fatalf("the fintech pack does not load: %v", err)
	}
	return p
}

// escalateGraphID finds the pack's graph of three write nodes in a chain by its shape, never by a literal ID.
func escalateGraphID(t testing.TB, p *trustpack.Pack, ids []string) (string, trustpack.GraphData) {
	t.Helper()
	for _, id := range ids {
		g, err := p.ActionGraphData(id)
		if err != nil {
			continue
		}
		w := 0
		for _, n := range g.Nodes {
			if n.Kind == "WRITE" {
				w++
			}
		}
		if w == 3 {
			return id, g
		}
	}
	t.Fatal("no three-write graph in the pack")
	return "", trustpack.GraphData{}
}

func graphIDs(t testing.TB) []string {
	t.Helper()
	var pf struct {
		Files []string `json:"action_graph_files"`
	}
	b, err := os.ReadFile(filepath.Join(packsDir(), packFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonUnmarshal(b, &pf); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range pf.Files {
		gb, err := os.ReadFile(filepath.Join(packsDir(), f))
		if err != nil {
			t.Fatal(err)
		}
		var g struct {
			ID string `json:"graph_id"`
		}
		if err := jsonUnmarshal(gb, &g); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, g.ID)
	}
	return ids
}

type fixedFacts struct {
	scope predicate.Scope
	env   predicate.Env
}

func (f fixedFacts) Scope() predicate.Scope { return f.scope }
func (f fixedFacts) Env() predicate.Env     { return f.env }

type factsFn func(ctx context.Context, q intake.RunnableQuery) (runnable.Facts, error)

func (f factsFn) FactsOf(ctx context.Context, q intake.RunnableQuery) (runnable.Facts, error) {
	return f(ctx, q)
}

// factsFor builds the facts the escalation graph's predicates read, as the evidence would give them: a resolved subject, the epoch, and the cases the tenant holds.
func factsFor(status string, epoch int64, cases []predicate.Row, vars map[string]predicate.Value) fixedFacts {
	scope := predicate.Scope{
		Variables: map[string]predicate.Type{
			"subject_resource.status": predicate.EnumOf("RESOLVED", "PENDING"), "subject_resource": predicate.Reference, "logical_resolution_epoch": predicate.Integer,
			"case_id": predicate.Reference, "evidence_packet_hash": predicate.String,
		},
		Entities: map[string]map[string]predicate.Type{
			"case":          {"id": predicate.Reference, "subject": predicate.Reference, "epoch": predicate.Integer, "status": predicate.EnumOf("OPEN", "ROUTED", "CLOSED")},
			"complaint_ref": {"subject": predicate.Reference, "epoch": predicate.Integer},
		},
	}
	v := map[string]predicate.Value{"subject_resource.status": predicate.Enum(status), "subject_resource": predicate.Ref("res_synthetic_1"), "logical_resolution_epoch": predicate.Int(epoch)}
	for k, x := range vars {
		v[k] = x
	}
	return fixedFacts{scope: scope, env: predicate.Env{Variables: v, Entities: map[string][]predicate.Row{"case": cases, "complaint_ref": nil}, Now: 1000}}
}

func adapterOver(t testing.TB, f runnable.Facts) (runnableport.Adapter, trustpack.GraphData) {
	t.Helper()
	p := loadPack(t)
	id, g := escalateGraphID(t, p, graphIDs(t))
	graphs := runnableport.PackGraphs{Pack: p, GraphID: func(n string) (string, bool) { return id, n == node }}
	return runnableport.Adapter{Graphs: graphs, Facts: factsFn(func(context.Context, intake.RunnableQuery) (runnable.Facts, error) { return f, nil })}, g
}

func query() intake.RunnableQuery {
	return intake.RunnableQuery{TenantID: tenant, IntentID: "int_synth01", IntentVersion: 1, IntentNode: node, SnapshotID: "evs_synth01"}
}

func capsOf(g trustpack.GraphData, nodeIDs ...string) []string {
	var out []string
	for _, n := range g.Nodes {
		for _, id := range nodeIDs {
			if n.NodeID == id {
				out = append(out, n.CapabilityID)
			}
		}
	}
	sort.Strings(out)
	return out
}

// REQ-AG-001, A511: on the first evaluation of an escalation (a resolved subject, no case yet) only the root of the chain is runnable: the later steps need the case that the root
// creates, so they are not runnable and are not an error.
func TestOnlyTheRootOfTheEscalationChainIsRunnableBeforeTheCaseExists(t *testing.T) {
	a, g := adapterOver(t, factsFor("RESOLVED", 1, nil, nil))
	got, err := a.Runnable(tctx(t), query())
	if err != nil {
		t.Fatalf("Runnable: %v", err)
	}
	if want := capsOf(g, g.Roots...); !reflect.DeepEqual(got, want) {
		t.Errorf("Runnable = %v; want the root's capability %v", got, want)
	}
}

// A precondition that is definitely false makes the step not runnable: an open case for the subject and epoch means the root may not create another. The answer is the empty set
// and no error: nothing is runnable, which is the question's own answer (A531).
func TestAnOpenCaseForTheSubjectMakesTheRootNotRunnableAndIsNotAnError(t *testing.T) {
	open := predicate.Row{"id": predicate.Ref("cas_synth01"), "subject": predicate.Ref("res_synthetic_1"), "epoch": predicate.Int(1), "status": predicate.Enum("OPEN")}
	a, _ := adapterOver(t, factsFor("RESOLVED", 1, []predicate.Row{open}, nil))
	got, err := a.Runnable(tctx(t), query())
	if err != nil || len(got) != 0 {
		t.Errorf("Runnable = %v, %v; want no step and no error", got, err)
	}
}

// With the case present and the evidence packet hash known, the whole chain after the root is runnable only when its dependencies are; the root's own precondition (no open case
// for the subject and epoch) is false here, so nothing downstream is runnable either: a step never runs before its dependency.
func TestADependentStepIsNotRunnableWhenItsDependencyIsNot(t *testing.T) {
	open := predicate.Row{"id": predicate.Ref("cas_synth01"), "subject": predicate.Ref("res_synthetic_1"), "epoch": predicate.Int(1), "status": predicate.Enum("OPEN")}
	vars := map[string]predicate.Value{"case_id": predicate.Ref("cas_synth01"), "evidence_packet_hash": predicate.Str("sha256:synthetic")}
	a, _ := adapterOver(t, factsFor("RESOLVED", 1, []predicate.Row{open}, vars))
	got, err := a.Runnable(tctx(t), query())
	if err != nil || len(got) != 0 {
		t.Errorf("Runnable = %v, %v; want no step and no error", got, err)
	}
}

// INV-16: an empty answer that comes with steps nobody could evaluate is an error and never "none". Facts that declare none of the variables the predicates read leave every step
// unevaluable.
func TestNothingEvaluableIsAnErrorNeverNone(t *testing.T) {
	bare := fixedFacts{scope: predicate.Scope{}, env: predicate.Env{Now: 1000}}
	a, _ := adapterOver(t, bare)
	got, err := a.Runnable(tctx(t), query())
	if !errors.Is(err, runnableport.ErrNotEvaluable) || len(got) != 0 {
		t.Errorf("Runnable = %v, %v; want no ids and ErrNotEvaluable", got, err)
	}
}

// The facts source failing, returning nothing, or returning nil facts is an error: never "none" and never "all".
func TestAFailingFactsSourceIsAnErrorNeverNoneOrAll(t *testing.T) {
	p := loadPack(t)
	id, _ := escalateGraphID(t, p, graphIDs(t))
	graphs := runnableport.PackGraphs{Pack: p, GraphID: func(n string) (string, bool) { return id, n == node }}
	boom := errors.New("synthetic source failure")
	for name, fn := range map[string]factsFn{
		"an error":  func(context.Context, intake.RunnableQuery) (runnable.Facts, error) { return nil, boom },
		"nil facts": func(context.Context, intake.RunnableQuery) (runnable.Facts, error) { return nil, nil },
	} {
		t.Run(name, func(t *testing.T) {
			got, err := runnableport.Adapter{Graphs: graphs, Facts: fn}.Runnable(tctx(t), query())
			if err == nil || got != nil {
				t.Errorf("Runnable = %v, %v; want no ids and an error", got, err)
			}
		})
	}
}

// A node with no graph, a graph that cannot be read and a missing seam are errors.
func TestAMissingGraphOrSeamIsAnError(t *testing.T) {
	p := loadPack(t)
	id, _ := escalateGraphID(t, p, graphIDs(t))
	facts := factsFn(func(context.Context, intake.RunnableQuery) (runnable.Facts, error) {
		return factsFor("RESOLVED", 1, nil, nil), nil
	})
	none := runnableport.PackGraphs{Pack: p, GraphID: func(string) (string, bool) { return "", false }}
	unknown := runnableport.PackGraphs{Pack: p, GraphID: func(string) (string, bool) { return id + "_absent", true }}
	cases := []struct {
		name string
		a    runnableport.Adapter
		want error
	}{
		{"a node with no graph", runnableport.Adapter{Graphs: none, Facts: facts}, runnableport.ErrNoGraph},
		{"a graph the pack does not have", runnableport.Adapter{Graphs: unknown, Facts: facts}, runnableport.ErrNoGraph},
		{"no graphs seam", runnableport.Adapter{Facts: facts}, runnableport.ErrConfig},
		{"no facts seam", runnableport.Adapter{Graphs: none}, runnableport.ErrConfig},
		{"the zero adapter", runnableport.Adapter{}, runnableport.ErrConfig},
		{"a pack graphs with no pack", runnableport.Adapter{Graphs: runnableport.PackGraphs{GraphID: func(string) (string, bool) { return id, true }}, Facts: facts}, runnableport.ErrConfig},
		{"a pack graphs with no mapping", runnableport.Adapter{Graphs: runnableport.PackGraphs{Pack: p}, Facts: facts}, runnableport.ErrConfig},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.a.Runnable(tctx(t), query())
			if !errors.Is(err, c.want) || got != nil {
				t.Errorf("Runnable = %v, %v; want no ids and %v", got, err, c.want)
			}
		})
	}
}

// A question that names no tenant, intent, node or snapshot is refused before a seam is asked; a cancelled context is its error.
func TestAQuestionWithAMissingPartIsRefusedBeforeAnySeamIsAsked(t *testing.T) {
	asked := false
	spy := factsFn(func(context.Context, intake.RunnableQuery) (runnable.Facts, error) {
		asked = true
		return factsFor("RESOLVED", 1, nil, nil), nil
	})
	p := loadPack(t)
	id, _ := escalateGraphID(t, p, graphIDs(t))
	a := runnableport.Adapter{Graphs: runnableport.PackGraphs{Pack: p, GraphID: func(string) (string, bool) { return id, true }}, Facts: spy}
	for name, mut := range map[string]func(*intake.RunnableQuery){
		"no tenant":   func(q *intake.RunnableQuery) { q.TenantID = "" },
		"no intent":   func(q *intake.RunnableQuery) { q.IntentID = "" },
		"no version":  func(q *intake.RunnableQuery) { q.IntentVersion = 0 },
		"no node":     func(q *intake.RunnableQuery) { q.IntentNode = "" },
		"no snapshot": func(q *intake.RunnableQuery) { q.SnapshotID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			q := query()
			mut(&q)
			got, err := a.Runnable(tctx(t), q)
			if !errors.Is(err, runnableport.ErrQuery) || got != nil || asked {
				t.Errorf("Runnable = %v, %v (a seam asked: %v); want no ids, ErrQuery and no seam asked", got, err, asked)
			}
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := a.Runnable(cancelled, query()); !errors.Is(err, context.Canceled) || got != nil {
		t.Errorf("Runnable on a cancelled context = %v, %v; want no ids and context.Canceled", got, err)
	}
}

// The facts the adapter asks for are the query's own: the same tenant, intent, node and snapshot, and the caller's facts, which the source may read and never changes.
func TestTheFactsSourceReceivesTheQueryUnchanged(t *testing.T) {
	var seen intake.RunnableQuery
	p := loadPack(t)
	id, _ := escalateGraphID(t, p, graphIDs(t))
	a := runnableport.Adapter{
		Graphs: runnableport.PackGraphs{Pack: p, GraphID: func(string) (string, bool) { return id, true }},
		Facts: factsFn(func(_ context.Context, q intake.RunnableQuery) (runnable.Facts, error) {
			seen = q
			return factsFor("RESOLVED", 1, nil, nil), nil
		}),
	}
	q := query()
	q.Facts = map[string]string{"case_id": "cas_synth01"}
	if _, err := a.Runnable(tctx(t), q); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, q) {
		t.Errorf("the source saw %+v; want %+v", seen, q)
	}
}

// INV-03, REQ-AG-001: over random facts the answer is deterministic, sorted, free of repeats, and a subset of the graph's own capabilities. It is never more than the graph holds.
func TestTheAnswerIsAlwaysASortedSubsetOfTheGraphsCapabilities(t *testing.T) {
	p := loadPack(t)
	id, g := escalateGraphID(t, p, graphIDs(t))
	own := map[string]bool{}
	for _, n := range g.Nodes {
		own[n.CapabilityID] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		status := rapid.SampledFrom([]string{"RESOLVED", "PENDING"}).Draw(rt, "status")
		epoch := rapid.Int64Range(0, 3).Draw(rt, "epoch")
		var cases []predicate.Row
		for i := 0; i < rapid.IntRange(0, 2).Draw(rt, "cases"); i++ {
			cases = append(cases, predicate.Row{"id": predicate.Ref("cas_synth0" + string(rune('1'+i))), "subject": predicate.Ref("res_synthetic_1"),
				"epoch": predicate.Int(rapid.Int64Range(0, 3).Draw(rt, "caseEpoch")), "status": predicate.Enum(rapid.SampledFrom([]string{"OPEN", "ROUTED", "CLOSED"}).Draw(rt, "caseStatus"))})
		}
		vars := map[string]predicate.Value{}
		if rapid.Bool().Draw(rt, "haveCase") {
			vars["case_id"] = predicate.Ref("cas_synth01")
		}
		if rapid.Bool().Draw(rt, "havePacket") {
			vars["evidence_packet_hash"] = predicate.Str("sha256:synthetic")
		}
		f := factsFor(status, epoch, cases, vars)
		a := runnableport.Adapter{Graphs: runnableport.PackGraphs{Pack: p, GraphID: func(string) (string, bool) { return id, true }},
			Facts: factsFn(func(context.Context, intake.RunnableQuery) (runnable.Facts, error) { return f, nil })}
		first, err1 := a.Runnable(tctx(t), query())
		second, err2 := a.Runnable(tctx(t), query())
		if (err1 == nil) != (err2 == nil) || !reflect.DeepEqual(first, second) {
			rt.Fatalf("not deterministic: %v %v / %v %v", first, err1, second, err2)
		}
		if !sort.StringsAreSorted(first) {
			rt.Fatalf("not sorted: %v", first)
		}
		seen := map[string]bool{}
		for _, c := range first {
			if !own[c] || seen[c] {
				rt.Fatalf("%q is not a capability of the graph, or is repeated: %v", c, first)
			}
			seen[c] = true
		}
	})
}

// PackGraphs: the graph of the node is the pack graph's data, node for node, edge for edge, and the answer is a copy.
func TestPackGraphsGivesThePackGraphAsRunnableData(t *testing.T) {
	p := loadPack(t)
	id, want := escalateGraphID(t, p, graphIDs(t))
	g, err := runnableport.PackGraphs{Pack: p, GraphID: func(string) (string, bool) { return id, true }}.GraphOf(tctx(t), tenant, node)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Roots, want.Roots) || len(g.Nodes) != len(want.Nodes) || len(g.Edges) != len(want.Edges) {
		t.Fatalf("GraphOf = %+v; want the shape of %+v", g, want)
	}
	for i, n := range want.Nodes {
		got := g.Nodes[i]
		if got.NodeID != n.NodeID || got.Kind != n.Kind || got.CapabilityID != n.CapabilityID || !reflect.DeepEqual(got.Preconditions, n.Preconditions) {
			t.Errorf("node %d = %+v; want %+v", i, got, n)
		}
	}
	for i, e := range want.Edges {
		if g.Edges[i].NodeID != e.NodeID || g.Edges[i].DependsOn != e.DependsOn || g.Edges[i].Condition != e.Condition {
			t.Errorf("edge %d = %+v; want %+v", i, g.Edges[i], e)
		}
	}
}

// REQ-AG-001: a graph whose steps have no precondition is runnable whole, root first and in any case sorted, with no error: the answer is the graph's capabilities and nothing else.
func TestAGraphOfReadsIsRunnableWholeAndWithoutAnError(t *testing.T) {
	p := loadPack(t)
	var id string
	var g trustpack.GraphData
	for _, gid := range graphIDs(t) {
		data, err := p.ActionGraphData(gid)
		if err != nil {
			t.Fatal(err)
		}
		writes := false
		for _, n := range data.Nodes {
			writes = writes || n.Kind == "WRITE"
		}
		if !writes {
			id, g = gid, data
			break
		}
	}
	if id == "" {
		t.Fatal("no graph without a write in the pack")
	}
	bare := fixedFacts{scope: predicate.Scope{}, env: predicate.Env{Now: 1000}}
	a := runnableport.Adapter{Graphs: runnableport.PackGraphs{Pack: p, GraphID: func(string) (string, bool) { return id, true }},
		Facts: factsFn(func(context.Context, intake.RunnableQuery) (runnable.Facts, error) { return bare, nil })}
	got, err := a.Runnable(tctx(t), query())
	var all []string
	for _, n := range g.Nodes {
		all = append(all, n.CapabilityID)
	}
	sort.Strings(all)
	if err != nil || !reflect.DeepEqual(got, all) {
		t.Errorf("Runnable = %v, %v; want every capability of the graph %v and no error", got, err, all)
	}
}
