package runnable

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// SYNTHETIC facts for tests (REQ-AG-001, INV-03).
type factsFake struct {
	scope predicate.Scope
	env   predicate.Env
}

func (f factsFake) Scope() predicate.Scope { return f.scope }
func (f factsFake) Env() predicate.Env     { return f.env }

func newFacts(vars map[string]predicate.Value) factsFake {
	return factsFake{
		scope: predicate.Scope{Variables: map[string]predicate.Type{
			"balance": predicate.Integer, "kyc.status": predicate.EnumOf("OK", "FAILED"), "flag": predicate.Boolean}},
		env: predicate.Env{Variables: vars, Now: 1000},
	}
}

func okVars() map[string]predicate.Value {
	return map[string]predicate.Value{"balance": predicate.Int(50), "kyc.status": predicate.Enum("OK"), "flag": predicate.Bool(true)}
}

func node(id, kind string, pre ...string) Node {
	return Node{NodeID: id, Kind: kind, CapabilityID: "cap." + id, Preconditions: pre}
}

func nodeErrs(t *testing.T, err error) map[string]bool {
	t.Helper()
	var ne *Errors
	if !errors.As(err, &ne) {
		t.Fatalf("want *Errors, got %v", err)
	}
	m := map[string]bool{}
	for _, n := range ne.Nodes {
		m[n.NodeID] = true
	}
	return m
}

func TestAllTrue(t *testing.T) {
	g := Graph{Roots: []string{"a"}, Nodes: []Node{node("b", "WRITE", "balance > 10"), node("a", "READ", "flag == true")}, Edges: []Edge{{NodeID: "b", DependsOn: "a"}}}
	got, err := Runnable(g, newFacts(okVars()))
	if err != nil || !reflect.DeepEqual(got, []string{"cap.a", "cap.b"}) {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestOneFalse(t *testing.T) {
	g := Graph{Roots: []string{"a", "c"}, Nodes: []Node{node("a", "READ", "flag == true"), node("c", "READ", "balance > 100")}}
	got, err := Runnable(g, newFacts(okVars()))
	if err != nil || !reflect.DeepEqual(got, []string{"cap.a"}) {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestDependencyChain(t *testing.T) {
	g := Graph{Roots: []string{"a"}, Nodes: []Node{node("a", "READ", "balance > 100"), node("b", "INTERNAL", "flag == true"), node("c", "WRITE", "flag == true")},
		Edges: []Edge{{NodeID: "b", DependsOn: "a"}, {NodeID: "c", DependsOn: "b"}}}
	got, err := Runnable(g, newFacts(okVars()))
	if err != nil || len(got) != 0 {
		t.Fatalf("unmet root must block the chain: %v %v", got, err)
	}
}

func TestEdgeCondition(t *testing.T) {
	g := Graph{Roots: []string{"a"}, Nodes: []Node{node("a", "READ", "flag == true"), node("b", "READ", "flag == true")},
		Edges: []Edge{{NodeID: "b", DependsOn: "a", Condition: "balance > 100"}}}
	got, err := Runnable(g, newFacts(okVars()))
	if err != nil || !reflect.DeepEqual(got, []string{"cap.a"}) {
		t.Fatalf("false edge condition must block: %v %v", got, err)
	}
}

func TestMissingFactNotRunnable(t *testing.T) {
	vars := okVars()
	delete(vars, "balance")
	// "not (balance > 10)" would be true on a null by naive evaluation; it must still be unknown.
	g := Graph{Roots: []string{"a", "b", "c"}, Nodes: []Node{node("a", "READ", "balance > 10"), node("b", "READ", "balance == 5"), node("c", "READ", "flag == true")}}
	got, err := Runnable(g, newFacts(vars))
	if !reflect.DeepEqual(got, []string{"cap.c"}) {
		t.Fatalf("got %v", got)
	}
	if m := nodeErrs(t, err); !m["a"] || !m["b"] || m["c"] {
		t.Fatalf("errors %v", m)
	}
	g2 := Graph{Roots: []string{"a"}, Nodes: []Node{node("a", "READ", "not (balance == 5)")}}
	got, _ = Runnable(g2, newFacts(vars))
	if len(got) != 0 {
		t.Fatalf("a negated null comparison must not be runnable: %v", got)
	}
}

func TestUnknownVariable(t *testing.T) {
	g := Graph{Roots: []string{"a", "b"}, Nodes: []Node{node("a", "READ", "nonesuch > 1"), node("b", "READ", "flag == true")}}
	got, err := Runnable(g, newFacts(okVars()))
	if !reflect.DeepEqual(got, []string{"cap.b"}) || !nodeErrs(t, err)["a"] {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestTypeError(t *testing.T) {
	g := Graph{Roots: []string{"a", "b", "c"}, Nodes: []Node{node("a", "READ", "balance == 'x'"), node("b", "READ", "flag == true"), node("c", "READ", "balance + 1")}}
	got, err := Runnable(g, newFacts(okVars()))
	if !reflect.DeepEqual(got, []string{"cap.b"}) {
		t.Fatalf("got %v", got)
	}
	if m := nodeErrs(t, err); !m["a"] || !m["c"] {
		t.Fatalf("errors %v", m)
	}
}

func TestSyntaxErrorNotRunnable(t *testing.T) {
	g := Graph{Roots: []string{"a"}, Nodes: []Node{node("a", "READ", "balance >>")}}
	got, err := Runnable(g, newFacts(okVars()))
	if len(got) != 0 || !nodeErrs(t, err)["a"] {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestWriteNeverRunnableWithUnmetReadPrecondition(t *testing.T) {
	g := Graph{Roots: []string{"r"}, Nodes: []Node{node("r", "READ", "kyc.status == 'OK'"), node("w", "WRITE", "kyc.status == 'OK'")}, Edges: []Edge{{NodeID: "w", DependsOn: "r"}}}
	v := okVars()
	v["kyc.status"] = predicate.Enum("FAILED")
	got, err := Runnable(g, newFacts(v))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestWriteWithoutPreconditionNotRunnable(t *testing.T) {
	g := Graph{Roots: []string{"w"}, Nodes: []Node{node("w", "WRITE")}}
	got, err := Runnable(g, newFacts(okVars()))
	if len(got) != 0 || !nodeErrs(t, err)["w"] {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestStructuralFailuresClosed(t *testing.T) {
	ok := func(id string) Node { return node(id, "READ", "flag == true") }
	cases := []struct {
		name string
		g    Graph
		want []string
	}{
		{"cycle", Graph{Roots: []string{"x"}, Nodes: []Node{ok("a"), ok("b"), ok("x")}, Edges: []Edge{{NodeID: "a", DependsOn: "b"}, {NodeID: "b", DependsOn: "a"}}}, []string{"cap.x"}},
		{"missing dep", Graph{Roots: []string{"a"}, Nodes: []Node{ok("a")}, Edges: []Edge{{NodeID: "a", DependsOn: "ghost"}}}, nil},
		{"orphan", Graph{Roots: []string{"x"}, Nodes: []Node{ok("x"), ok("o")}}, []string{"cap.x"}},
		{"unknown kind", Graph{Roots: []string{"a"}, Nodes: []Node{node("a", "MAGIC", "flag == true")}}, nil},
		{"duplicate node", Graph{Roots: []string{"a"}, Nodes: []Node{ok("a"), ok("a")}}, nil},
	}
	for _, c := range cases {
		got, err := Runnable(c.g, newFacts(okVars()))
		if err == nil {
			t.Errorf("%s: want an error", c.name)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestNilFacts(t *testing.T) {
	got, err := Runnable(Graph{Roots: []string{"a"}, Nodes: []Node{node("a", "READ", "flag == true")}}, nil)
	if len(got) != 0 || err == nil {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestDeterministicAndNotMutated(t *testing.T) {
	g := Graph{Roots: []string{"a", "z"}, Nodes: []Node{node("z", "READ", "flag == true"), node("m", "WRITE", "balance > 1"), node("a", "READ", "flag == true")},
		Edges: []Edge{{NodeID: "m", DependsOn: "a"}}}
	f := newFacts(okVars())
	before := Graph{Roots: append([]string(nil), g.Roots...), Nodes: append([]Node(nil), g.Nodes...), Edges: append([]Edge(nil), g.Edges...)}
	first, _ := Runnable(g, f)
	for i := 0; i < 20; i++ {
		again, _ := Runnable(g, f)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("not deterministic: %v vs %v", first, again)
		}
	}
	if !reflect.DeepEqual(first, []string{"cap.a", "cap.m", "cap.z"}) {
		t.Fatalf("not sorted/complete: %v", first)
	}
	if !reflect.DeepEqual(g, before) || !reflect.DeepEqual(f.env.Variables, okVars()) {
		t.Fatal("input mutated")
	}
}

func entityFacts(entities map[string][]predicate.Row) factsFake {
	f := newFacts(okVars())
	f.scope.Entities = map[string]map[string]predicate.Type{"ticket": {"status": predicate.EnumOf("OPEN", "CLOSED")}}
	f.env.Entities = entities
	return f
}

func TestAbsentEntityIsUnknownButEmptyEntityIsKnown(t *testing.T) {
	g := Graph{Roots: []string{"a"}, Nodes: []Node{node("a", "READ", "not exists(ticket where ticket.status == 'OPEN')")}}
	got, err := Runnable(g, entityFacts(nil))
	if len(got) != 0 || !nodeErrs(t, err)["a"] {
		t.Fatalf("an entity the snapshot lacks must be unknown: %v %v", got, err)
	}
	got, err = Runnable(g, entityFacts(map[string][]predicate.Row{"ticket": {}}))
	if err != nil || !reflect.DeepEqual(got, []string{"cap.a"}) {
		t.Fatalf("an empty entity is a known fact: %v %v", got, err)
	}
	got, _ = Runnable(g, entityFacts(map[string][]predicate.Row{"ticket": {{"status": predicate.Enum("OPEN")}}}))
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestEvaluationErrorNotRunnable(t *testing.T) {
	v := okVars()
	v["balance"] = predicate.Int(predicate.MaxInteger)
	g := Graph{Roots: []string{"a"}, Nodes: []Node{node("a", "READ", "balance + 1 > 0")}}
	got, err := Runnable(g, newFacts(v))
	if len(got) != 0 || !nodeErrs(t, err)["a"] {
		t.Fatalf("overflow must be unknown: %v %v", got, err)
	}
}
