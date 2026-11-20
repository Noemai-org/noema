package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

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
	once    sync.Once
	stopped atomic.Bool
	w       temporalWorker
	c       Connection
}

// Stop stops the worker and closes the connection, once.
func (r *RunningWorker) Stop() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		r.stopped.Store(true)
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

// fataler is the optional part of a Connection that reports a worker that died on its own (worker_dial.go; REQ-OPS-003, AGENT-07).
type fataler interface {
	Fatal() <-chan error
	Ready() bool
}

// Fatal delivers, once, the error that stopped the worker on its own. main selects on it and exits non-zero so Compose restarts the process (A442, interim, relayed, not yet
// committed). A connection with no fatal hook, or a nil handle, never delivers (a nil channel blocks).
func (r *RunningWorker) Fatal() <-chan error {
	if r == nil {
		return nil
	}
	if f, ok := r.c.(fataler); ok {
		return f.Fatal()
	}
	return nil
}

// Ready is false for no worker, a stopped worker and a worker that died on its own.
func (r *RunningWorker) Ready() bool {
	if r == nil || r.stopped.Load() {
		return false
	}
	if f, ok := r.c.(fataler); ok {
		return f.Ready()
	}
	return true
}

// Check is the readiness of the worker alone: it names `worker` when it is down.
func (r *RunningWorker) Check() (bool, string) {
	if !r.Ready() {
		return false, "worker is not running"
	}
	return true, ""
}

// withWorker adds the worker to a readiness check: the first reason of next wins, then the worker's. There is no degraded mode (A442).
func withWorker(next readiness, r *RunningWorker) readiness {
	return func() (bool, string) {
		if ok, reason := next(); !ok {
			return false, reason
		}
		return r.Check()
	}
}
