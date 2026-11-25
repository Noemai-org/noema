// Package runnableport implements intake.Runnable (A363, A511, A684): which capabilities of the intent node's action graph have their preconditions met now. It joins two seams the
// composition fills, the graph of an intent node and the facts of the evidence snapshot, to the pure `runnable` package, which evaluates every predicate with internal/predicate
// and nothing else. The package names no capability, node or graph (INV-14) and reads no clock, store or model (INV-01).
package runnableport

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/runnable"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// The refusals. Callers compare with errors.Is.
var (
	// ErrConfig is returned by an adapter built without a seam: nothing is ever answered from nothing.
	ErrConfig = errors.New("runnableport: the adapter is not configured")
	// ErrQuery is returned for a question that names no tenant, intent, version, node or snapshot. No seam is asked.
	ErrQuery = errors.New("runnableport: the question names no tenant, intent, version, node or snapshot")
	// ErrNoGraph is returned for an intent node that has no action graph.
	ErrNoGraph = errors.New("runnableport: there is no action graph for the intent node")
	// ErrNotEvaluable is returned when nothing is runnable and the start of the graph could not be evaluated on the facts: that is not the answer "none", it is no answer.
	ErrNotEvaluable = errors.New("runnableport: the start of the graph could not be evaluated on the facts")
)

// Graphs gives the action graph of an intent node, as data. An intent node with no graph is ErrNoGraph.
type Graphs interface {
	GraphOf(ctx context.Context, tenantID, intentNode string) (runnable.Graph, error)
}

// FactsSource gives the facts a graph's predicates read, for one evaluation. It reads the sealed snapshot the query names and the caller's facts, and answers nothing it did not
// read: a fact it cannot read is absent from the facts, never guessed.
type FactsSource interface {
	FactsOf(ctx context.Context, q intake.RunnableQuery) (runnable.Facts, error)
}

// Adapter implements intake.Runnable. The zero value refuses every call with ErrConfig.
type Adapter struct {
	Graphs Graphs
	Facts  FactsSource
}

var _ intake.Runnable = Adapter{}

// Runnable answers which capabilities of the intent node's graph may run now, sorted and without repeats, never more than the graph holds.
//
// A step the facts cannot evaluate is not runnable. The answer is the capabilities that are. When none is, the answer is "none" (an empty set and no error) unless the start of the
// graph, a root, could not be evaluated, or the graph itself is defective (a node that is unknown or repeated, or an error that names no node): then there is no answer and the
// error is ErrNotEvaluable. A later step that reads what an earlier one creates is unevaluable by design on a first evaluation and does not make the answer an error. A source that
// fails or gives no facts, a node with no graph and a question missing a part are errors, and an error is never "none" and never "all" (the contract of intake.Runnable).
func (a Adapter) Runnable(ctx context.Context, q intake.RunnableQuery) ([]string, error) {
	if a.Graphs == nil || a.Facts == nil {
		return nil, ErrConfig
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.TenantID == "" || q.IntentID == "" || q.IntentVersion <= 0 || q.IntentNode == "" || q.SnapshotID == "" {
		return nil, ErrQuery
	}
	g, err := a.Graphs.GraphOf(ctx, q.TenantID, q.IntentNode)
	if err != nil {
		return nil, fmt.Errorf("runnableport: read the graph: %w", err)
	}
	f, err := a.Facts.FactsOf(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("runnableport: read the facts: %w", err)
	}
	if f == nil {
		return nil, fmt.Errorf("%w: the facts source gave no facts", ErrNotEvaluable)
	}
	ids, err := runnable.Runnable(g, f)
	if err == nil {
		return append([]string{}, ids...), nil
	}
	var ne *runnable.Errors
	if !errors.As(err, &ne) {
		return nil, fmt.Errorf("runnableport: evaluate the graph: %w", err)
	}
	if len(ids) > 0 || !startUnevaluable(g, ne) {
		return append([]string{}, ids...), nil
	}
	return nil, fmt.Errorf("%w: %d steps could not be evaluated", ErrNotEvaluable, len(ne.Nodes))
}

// startUnevaluable reports whether the unevaluable steps include a root, an error that names no node or a node the graph does not have, or a defect of the graph that makes every
// answer suspect (a repeated node, a cycle).
func startUnevaluable(g runnable.Graph, ne *runnable.Errors) bool {
	roots := make(map[string]bool, len(g.Roots))
	for _, r := range g.Roots {
		roots[r] = true
	}
	known := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		known[n.NodeID] = true
	}
	for _, e := range ne.Nodes {
		if e.NodeID == "" || roots[e.NodeID] || !known[e.NodeID] || e.Reason == "duplicate node id" || e.Reason == "dependency cycle" {
			return true
		}
	}
	return false
}

// PackGraphs is a Graphs over the verified pack. GraphID names the pack graph of an intent node: the composition sets it from its configuration, and from the node's action graph once
// the pack names one. The graph is read through the pack's accessor, as a copy.
type PackGraphs struct {
	Pack    *trustpack.Pack
	GraphID func(intentNode string) (string, bool)
}

var _ Graphs = PackGraphs{}

// GraphOf gives the graph of the intent node as runnable data. A missing pack or mapping is ErrConfig, no tenant is ErrQuery, and a node with no graph, or one the pack does not
// have, is ErrNoGraph.
func (g PackGraphs) GraphOf(ctx context.Context, tenantID, intentNode string) (runnable.Graph, error) {
	if g.Pack == nil || g.GraphID == nil {
		return runnable.Graph{}, ErrConfig
	}
	if err := ctx.Err(); err != nil {
		return runnable.Graph{}, err
	}
	if tenantID == "" || intentNode == "" {
		return runnable.Graph{}, ErrQuery
	}
	id, ok := g.GraphID(intentNode)
	if !ok {
		return runnable.Graph{}, ErrNoGraph
	}
	data, err := g.Pack.ActionGraphData(id)
	switch {
	case errors.Is(err, trustpack.ErrGraph):
		return runnable.Graph{}, fmt.Errorf("%w: %w", ErrNoGraph, err)
	case err != nil:
		return runnable.Graph{}, err
	}
	out := runnable.Graph{Roots: append([]string{}, data.Roots...)}
	for _, n := range data.Nodes {
		out.Nodes = append(out.Nodes, runnable.Node{NodeID: n.NodeID, Kind: n.Kind, CapabilityID: n.CapabilityID, Preconditions: n.Preconditions})
	}
	for _, e := range data.Edges {
		out.Edges = append(out.Edges, runnable.Edge{NodeID: e.NodeID, DependsOn: e.DependsOn, Condition: e.Condition})
	}
	return out, nil
}
