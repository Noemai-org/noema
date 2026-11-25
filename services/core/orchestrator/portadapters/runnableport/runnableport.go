// Package runnableport implements intake.Runnable (A363, A511, A684): which capabilities of the intent node's action graph have their preconditions met now. It joins two
// seams the composition fills (the graph of an intent node and the facts of the evidence snapshot) to the pure `runnable` package. Stub: the implementation follows the red.
package runnableport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/runnable"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// The refusals. Callers compare with errors.Is.
var (
	ErrConfig       = errors.New("runnableport: the adapter is not configured")
	ErrQuery        = errors.New("runnableport: the question names no tenant, intent, node or snapshot")
	ErrNoGraph      = errors.New("runnableport: there is no action graph for the intent node")
	ErrNotEvaluable = errors.New("runnableport: no step could be evaluated on the facts")
)

// Graphs gives the action graph of an intent node, as data.
type Graphs interface {
	GraphOf(ctx context.Context, tenantID, intentNode string) (runnable.Graph, error)
}

// FactsSource gives the facts a graph's predicates read, for one evaluation.
type FactsSource interface {
	FactsOf(ctx context.Context, q intake.RunnableQuery) (runnable.Facts, error)
}

// Adapter implements intake.Runnable.
type Adapter struct {
	Graphs Graphs
	Facts  FactsSource
}

var _ intake.Runnable = Adapter{}

// Runnable answers the intake's question. Stub.
func (a Adapter) Runnable(ctx context.Context, q intake.RunnableQuery) ([]string, error) {
	return nil, nil
}

// PackGraphs is a Graphs over the verified pack. GraphID names the pack graph of an intent node (the composition sets it from its configuration, and from the node's action graph
// once the pack names one).
type PackGraphs struct {
	Pack    *trustpack.Pack
	GraphID func(intentNode string) (string, bool)
}

var _ Graphs = PackGraphs{}

// GraphOf gives the graph of the intent node. Stub.
func (g PackGraphs) GraphOf(ctx context.Context, tenantID, intentNode string) (runnable.Graph, error) {
	return runnable.Graph{}, nil
}
