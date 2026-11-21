package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Noemai-org/noema/services/core/workflows"
)

// wire_worker.go: the worker's lane of the composition (STM-04; REQ-VER-004, REQ-EXE-006, REQ-EXE-005; INV-30, INV-07, AGENT-08, AGENT-16). buildWorker plugs the real ports of the
// workflows' activities into the worker of worker_run.go and answers readiness. It builds no port: the Controller behind the Executor, the case adapter behind the Reader and Trust behind
// the Issuer are made elsewhere and handed in. The workflows do no I/O (INV-30): every effect is one of these three ports, and a write activity is never retried by Temporal (D8).

// workerPorts are the real ports of the activities of `workflows` (EscalateActivities). All three are required: a worker with part of them would stop a case half way.
type workerPorts struct {
	Executor workflows.GrantExecutor
	Reader   workflows.CaseReader
	Issuer   workflows.RouteGrantIssuer
}

// workerHandle is a started worker. Readiness is true only while it runs: it turns false the moment Stop begins and when the worker dies on its own.
type workerHandle struct {
	rw    *RunningWorker
	drain time.Duration

	mu       sync.Mutex
	stopping bool // guarded by mu: no activity starts once set
	inflight sync.WaitGroup

	stopped  atomic.Bool
	died     atomic.Bool
	quit     chan struct{}
	stopOnce sync.Once
	stopErr  error
}

// enter registers one activity call. After Stop began it refuses (ErrWorkerStopping): nothing is written while the worker goes down. The returned func ends the call.
func (h *workerHandle) enter() (func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopping {
		return nil, ErrWorkerStopping
	}
	h.inflight.Add(1)
	return h.inflight.Done, nil
}

// Stop makes the worker not ready, waits for in-flight activities for at most the drain bound, then stops the worker and closes its connection, once. It returns
// ErrWorkerDrainTimeout when the bound passed with an activity still running (the worker is stopped all the same). Safe on nil and to call more than once.
func (h *workerHandle) Stop() error {
	if h == nil {
		return nil
	}
	h.stopOnce.Do(func() {
		h.mu.Lock()
		h.stopping = true
		h.mu.Unlock()
		h.stopped.Store(true)
		close(h.quit)
		idle := make(chan struct{})
		go func() { h.inflight.Wait(); close(idle) }()
		ctx, cancel := context.WithTimeout(context.Background(), h.drain)
		select {
		case <-idle:
		case <-ctx.Done():
			h.stopErr = ErrWorkerDrainTimeout
		}
		cancel()
		h.rw.Stop()
	})
	return h.stopErr
}

// watch marks the worker dead when its connection reports a fatal error, until Stop begins.
func (h *workerHandle) watch(c <-chan error) {
	select {
	case <-c:
		if !h.stopped.Load() {
			h.died.Store(true)
		}
	case <-h.quit:
	}
}

// Readiness is the /readyz check of the worker: not ready when there is no worker (none configured, or it did not start), when it has stopped or when it died on its own. The reason
// names the worker and never carries a cause.
func (h *workerHandle) Readiness() readiness {
	return func() (bool, string) {
		switch {
		case h == nil:
			return false, "worker is not running"
		case h.stopped.Load():
			return false, "worker has stopped"
		case h.died.Load():
			return false, "worker has died"
		}
		return true, ""
	}
}

// buildWorker reads the Temporal settings, binds a deadline of NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS to each port call and starts the worker. With none of the settings it returns no worker
// and no error. With some, a missing port, or any wrong (the drain bound NOEMA_TEMPORAL_SHUTDOWN_DRAIN_MS included), it returns an error that names it and dials nothing; a worker
// that does not start leaves no handle and no connection. The context must carry a deadline (startWorker refuses it otherwise).
func buildWorker(ctx context.Context, env Env, ports workerPorts, dial func(context.Context, *WorkerConfig) (Connection, error)) (*workerHandle, error) {
	cfg, err := LoadWorkerConfig(env)
	if err != nil || cfg == nil {
		return nil, err
	}
	drainMS, err := strconv.ParseInt(env.Getenv(envWorkerShutdownDrain), 10, 64)
	if err != nil || drainMS <= 0 || drainMS > int64(1<<31) {
		return nil, fmt.Errorf("cmd/core: %s: a positive whole number of milliseconds is required", envWorkerShutdownDrain)
	}
	switch {
	case ports.Executor == nil:
		return nil, errors.New("cmd/core: the worker needs the executor port for its activities")
	case ports.Reader == nil:
		return nil, errors.New("cmd/core: the worker needs the reader port for its activities")
	case ports.Issuer == nil:
		return nil, errors.New("cmd/core: the worker needs the issuer port for its activities")
	}
	h := &workerHandle{drain: time.Duration(drainMS) * time.Millisecond, quit: make(chan struct{})}
	acts := &workflows.EscalateActivities{
		Executor: boundedExecutor{ports.Executor, cfg.ActivityTimeout, h},
		Reader:   boundedReader{ports.Reader, cfg.ActivityTimeout, h},
		Issuer:   boundedIssuer{ports.Issuer, cfg.ActivityTimeout, h},
	}
	rw, err := startWorker(ctx, cfg, dial, acts)
	if err != nil {
		return nil, fmt.Errorf("cmd/core: %w", err)
	}
	h.rw = rw
	if fr, ok := rw.c.(fatalReporter); ok {
		go h.watch(fr.Fatal())
	}
	return h, nil
}

// The bounded ports: one deadline on every call (a context deadline on every call, AGENT-16: the configured timeout, no default), and every call counted so that Stop can drain. They add
// nothing else, and a call is made once.
type boundedExecutor struct {
	p workflows.GrantExecutor
	d time.Duration
	h *workerHandle
}

func (b boundedExecutor) ExecuteGrant(ctx context.Context, tenantID, grantID string) (workflows.GrantRun, error) {
	done, err := b.h.enter()
	if err != nil {
		return workflows.GrantRun{}, err
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, b.d)
	defer cancel()
	return b.p.ExecuteGrant(ctx, tenantID, grantID)
}

type boundedReader struct {
	p workflows.CaseReader
	d time.Duration
	h *workerHandle
}

func (b boundedReader) ReadCaseByKey(ctx context.Context, tenantID, key string) (workflows.CaseView, error) {
	done, err := b.h.enter()
	if err != nil {
		return workflows.CaseView{}, err
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, b.d)
	defer cancel()
	return b.p.ReadCaseByKey(ctx, tenantID, key)
}

type boundedIssuer struct {
	p workflows.RouteGrantIssuer
	d time.Duration
	h *workerHandle
}

func (b boundedIssuer) IssueRouteGrant(ctx context.Context, req workflows.RouteGrantRequest) (string, error) {
	done, err := b.h.enter()
	if err != nil {
		return "", err
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, b.d)
	defer cancel()
	return b.p.IssueRouteGrant(ctx, req)
}
