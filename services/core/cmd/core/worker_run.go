package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Noemai-org/noema/services/core/workflows"
)

// worker_run.go: starting and stopping the core's Temporal worker (STM-04 slice 2; card status/STM-04.md; REQ-EXE-006, INV-30, AGENT-16). The connection is a seam, so the order of events is
// tested without a server; worker_dial.go is the real connection. Nothing here reads an address or a file into an error text.

// ErrWorkerNotStarted marks a worker that was not started. The reason stays reachable with errors.Is and errors.As and is never part of the text (an address, a path or a server's
// answer must not reach a log through it).
var ErrWorkerNotStarted = errors.New("worker: the Temporal worker was not started")

type startError struct{ cause error }

func (e *startError) Error() string   { return ErrWorkerNotStarted.Error() }
func (e *startError) Unwrap() []error { return []error{ErrWorkerNotStarted, e.cause} }

func notStarted(cause error) error { return &startError{cause: cause} }

// temporalWorker is the part of a Temporal worker this process uses.
type temporalWorker interface {
	registry
	Start() error
	Stop()
}

// Connection is one connection to Temporal: it makes a worker for a task queue and closes.
type Connection interface {
	NewWorker(taskQueue string) temporalWorker
	Close()
}

// RunningWorker is a started worker and its connection. Stop is safe to call more than once.
type RunningWorker struct {
	once sync.Once
	w    temporalWorker
	c    Connection
}

// Stop stops the worker and closes the connection, once.
func (r *RunningWorker) Stop() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		r.w.Stop()
		r.c.Close()
	})
}

// startWorker dials, makes the worker for the configured task queue, registers exactly what `workflows` declares and starts it. Anything it cannot run (no configuration, no activities,
// no dialer, a context with no deadline) is refused before a dial; a worker that does not start closes the connection it opened.
func startWorker(ctx context.Context, cfg *WorkerConfig, dial func(context.Context, *WorkerConfig) (Connection, error), acts *workflows.EscalateActivities) (*RunningWorker, error) {
	if cfg == nil || acts == nil || dial == nil {
		return nil, notStarted(errors.New("a part is missing"))
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, notStarted(errors.New("the context needs a deadline"))
	}
	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, notStarted(fmt.Errorf("dial: %w", err))
	}
	w := conn.NewWorker(cfg.TaskQueue)
	registerWorker(w, acts)
	if err := w.Start(); err != nil {
		conn.Close()
		return nil, notStarted(fmt.Errorf("start: %w", err))
	}
	return &RunningWorker{w: w, c: conn}, nil
}
