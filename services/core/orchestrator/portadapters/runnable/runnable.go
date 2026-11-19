// Package runnable decides which steps of an action graph may run now (A363, interim, relayed, not yet committed).
//
// Implements REQ-AG-001 (graph preconditions) for the Trust adapter; SoT INV-03, INV-01, AGENT-09. Pure data in, data out:
// no I/O, no clock, no model. Predicates are evaluated only by internal/predicate.
//
// May import: the standard library and internal/predicate.
// Must never: import trust, execution, outcome or record; build a second evaluator; read the clock.
package runnable

import (
	"sort"
	"strings"
	"unicode"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// Graph is an action graph as data (action-graph.v2 fields the decision needs).
type Graph struct {
	Roots []string
	Nodes []Node
	Edges []Edge
}

// Node is one graph node.
type Node struct {
	NodeID        string
	Kind          string
	CapabilityID  string
	Preconditions []string
}

// Edge says NodeID depends on DependsOn. A non-empty Condition is one more predicate that must be true.
type Edge struct {
	NodeID    string
	DependsOn string
	Condition string
}

// Facts is the current evidence snapshot seen as predicate inputs.
type Facts interface {
	Scope() predicate.Scope
	Env() predicate.Env
}

// NodeError is why one node is not runnable.
type NodeError struct {
	NodeID string
	Reason string
}

// Errors lists every node that could not be evaluated. The ids returned beside it are still safe to use.
type Errors struct{ Nodes []NodeError }

func (e *Errors) Error() string { return "runnable: nodes not evaluable" }

// Runnable returns the sorted capability ids of the nodes runnable now: every precondition is true on the facts, every
// edge condition into the node is true, and every node it depends on is runnable too (the facts carry no execution
// history, so the safer reading is that a dependency must itself be runnable now). Anything that cannot be evaluated
// (unknown variable, type error, syntax error, a fact the snapshot lacks, a cycle, a missing dependency, a WRITE node
// without a precondition) is not runnable and is listed in the returned *Errors. The ids are valid even when the error
// is non-nil; a nil Facts yields no ids and an error. Pure and deterministic; the inputs are never modified.
func Runnable(g Graph, f Facts) ([]string, error) {
	if f == nil {
		return nil, &Errors{Nodes: []NodeError{{Reason: "no facts"}}}
	}
	r := &run{g: g, scope: f.Scope(), env: f.Env(), nodes: map[string]Node{}, deps: map[string][]Edge{}, state: map[string]state{}}
	r.index()
	var ids []string
	for _, n := range g.Nodes {
		if r.nodes[n.NodeID].NodeID != "" && r.runnable(n.NodeID) {
			ids = append(ids, n.CapabilityID)
		}
	}
	sort.Strings(ids)
	ids = dedupe(ids)
	if len(r.errs) == 0 {
		return ids, nil
	}
	sort.Slice(r.errs, func(i, j int) bool {
		if r.errs[i].NodeID != r.errs[j].NodeID {
			return r.errs[i].NodeID < r.errs[j].NodeID
		}
		return r.errs[i].Reason < r.errs[j].Reason
	})
	return ids, &Errors{Nodes: r.errs}
}

type state uint8

const (
	visiting state = iota + 1
	yes
	no
)

type run struct {
	g     Graph
	scope predicate.Scope
	env   predicate.Env
	nodes map[string]Node
	deps  map[string][]Edge
	dup   map[string]bool
	state map[string]state
	errs  []NodeError
}

func (r *run) fail(id, reason string) { r.errs = append(r.errs, NodeError{NodeID: id, Reason: reason}) }

func (r *run) index() {
	r.dup = map[string]bool{}
	for _, n := range r.g.Nodes {
		if _, seen := r.nodes[n.NodeID]; seen {
			r.dup[n.NodeID] = true
			continue
		}
		r.nodes[n.NodeID] = n
	}
	for id := range r.dup {
		delete(r.nodes, id)
		r.fail(id, "duplicate node id")
	}
	for _, e := range r.g.Edges {
		r.deps[e.NodeID] = append(r.deps[e.NodeID], e)
	}
}

func (r *run) isRoot(id string) bool {
	for _, x := range r.g.Roots {
		if x == id {
			return true
		}
	}
	return false
}

func (r *run) runnable(id string) bool {
	switch r.state[id] {
	case yes:
		return true
	case no:
		return false
	case visiting:
		r.fail(id, "dependency cycle")
		return false
	}
	r.state[id] = visiting
	ok := r.evaluate(id)
	if ok {
		r.state[id] = yes
	} else {
		r.state[id] = no
	}
	return ok
}

func (r *run) evaluate(id string) bool {
	n, found := r.nodes[id]
	if !found {
		r.fail(id, "unknown node")
		return false
	}
	switch n.Kind {
	case "READ", "INTERNAL":
	case "WRITE":
		if len(n.Preconditions) == 0 {
			r.fail(id, "a WRITE node has no precondition")
			return false
		}
	default:
		r.fail(id, "unknown node kind")
		return false
	}
	if len(r.deps[id]) == 0 && !r.isRoot(id) {
		r.fail(id, "neither a root nor has an incoming edge")
		return false
	}
	ok := true
	for _, p := range n.Preconditions {
		if !r.holds(id, p) {
			ok = false
		}
	}
	for _, e := range r.deps[id] {
		if _, known := r.nodes[e.DependsOn]; !known {
			r.fail(id, "depends on an unknown node")
			ok = false
			continue
		}
		if !r.runnable(e.DependsOn) {
			ok = false
		}
		if e.Condition != "" && !r.holds(id, e.Condition) {
			ok = false
		}
	}
	return ok
}

// holds evaluates one predicate with the existing engine. It is true only on a definite, boolean true.
func (r *run) holds(id, src string) bool {
	if missing := r.missing(src); missing != "" {
		r.fail(id, "fact not in the snapshot: "+missing)
		return false
	}
	p, err := predicate.Compile(src, r.scope)
	if err != nil {
		r.fail(id, "predicate refused: "+err.Error())
		return false
	}
	if p.Type().Kind != predicate.KindBoolean {
		r.fail(id, "predicate is not Boolean")
		return false
	}
	v, err := p.EvalBool(r.env)
	if err != nil {
		r.fail(id, "predicate unknown: "+err.Error())
		return false
	}
	return v
}

// missing returns the first declared variable or entity that the predicate names and the snapshot lacks. The evaluator
// answers some predicates over a null (for example not (x == 5)); a missing fact must stay unknown, never true.
func (r *run) missing(src string) string {
	for _, path := range paths(src) {
		if _, declared := r.scope.Variables[path]; declared && r.env.Variables[path].IsNull() {
			return path
		}
		head := path
		if i := strings.IndexByte(path, '.'); i >= 0 {
			head = path[:i]
		}
		if _, declared := r.scope.Entities[head]; declared {
			if _, present := r.env.Entities[head]; !present {
				return head
			}
		}
	}
	return ""
}

// paths lists the dotted identifier paths of a predicate, skipping string literals.
func paths(src string) []string {
	var out []string
	rs := []rune(src)
	for i := 0; i < len(rs); {
		switch c := rs[i]; {
		case c == '\'':
			i++
			for i < len(rs) && rs[i] != '\'' {
				i++
			}
			i++
		case identStart(c):
			j := i
			for j < len(rs) && (identStart(rs[j]) || unicode.IsDigit(rs[j]) || (rs[j] == '.' && j+1 < len(rs) && identStart(rs[j+1]))) {
				j++
			}
			out = append(out, string(rs[i:j]))
			i = j
		default:
			i++
		}
	}
	return out
}

func identStart(c rune) bool { return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }

func dedupe(in []string) []string {
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}
