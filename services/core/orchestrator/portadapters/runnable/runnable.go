// Package runnable decides which steps of an action graph may run now (A363, interim, relayed, not yet committed).
//
// Implements REQ-AG-001 (graph preconditions) for the Trust adapter; SoT INV-03, INV-01, AGENT-09. Pure data in, data out:
// no I/O, no clock, no model. Predicates are evaluated only by internal/predicate.
//
// May import: the standard library and internal/predicate.
// Must never: import trust, execution, outcome or record; build a second evaluator; read the clock.
package runnable

import "github.com/Noemai-org/noema/services/core/internal/predicate"

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

// Runnable returns the sorted capability ids of the nodes runnable now.
func Runnable(g Graph, f Facts) ([]string, error) { return nil, nil }
