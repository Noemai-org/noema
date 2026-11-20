package main

import (
	"context"
	"errors"
	"fmt"
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

// workerHandle is a started worker. Readiness is true only while it runs.
type workerHandle struct {
	rw      *RunningWorker
	stopped atomic.Bool
}

// Stop stops the worker and closes its connection, once. Safe on nil and to call more than once.
func (h *workerHandle) Stop() error {
	if h == nil {
		return nil
	}
	h.stopped.Store(true)
	h.rw.Stop()
	return nil
}

// Readiness is the /readyz check of the worker: not ready when there is no worker (none configured, or it did not start) or when it has stopped, and the reason names the worker.
func (h *workerHandle) Readiness() readiness {
	return func() (bool, string) {
		switch {
		case h == nil:
			return false, "worker is not running"
		case h.stopped.Load():
			return false, "worker has stopped"
		}
		return true, ""
	}
}

// buildWorker reads the Temporal settings, binds a deadline of NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS to each port call and starts the worker. With none of the settings it returns no worker
// and no error. With some, a missing port, or any wrong, it returns an error that names it and dials nothing; a worker that does not start leaves no handle and no connection. The
// context must carry a deadline (startWorker refuses it otherwise).
func buildWorker(ctx context.Context, env Env, ports workerPorts, dial func(context.Context, *WorkerConfig) (Connection, error)) (*workerHandle, error) {
	cfg, err := LoadWorkerConfig(env)
	if err != nil || cfg == nil {
		return nil, err
	}
	switch {
	case ports.Executor == nil:
		return nil, errors.New("cmd/core: the worker needs the executor port for its activities")
	case ports.Reader == nil:
		return nil, errors.New("cmd/core: the worker needs the reader port for its activities")
	case ports.Issuer == nil:
		return nil, errors.New("cmd/core: the worker needs the issuer port for its activities")
	}
	acts := &workflows.EscalateActivities{
		Executor: boundedExecutor{ports.Executor, cfg.ActivityTimeout},
		Reader:   boundedReader{ports.Reader, cfg.ActivityTimeout},
		Issuer:   boundedIssuer{ports.Issuer, cfg.ActivityTimeout},
	}
	rw, err := startWorker(ctx, cfg, dial, acts)
	if err != nil {
		return nil, fmt.Errorf("cmd/core: %w", err)
	}
	return &workerHandle{rw: rw}, nil
}

// The bounded ports: one deadline on every call (a context deadline on every call, AGENT-16: the configured timeout, no default). They add nothing else, and a call is made once.
type boundedExecutor struct {
	p workflows.GrantExecutor
	d time.Duration
}

func (b boundedExecutor) ExecuteGrant(ctx context.Context, tenantID, grantID string) (workflows.GrantRun, error) {
	ctx, cancel := context.WithTimeout(ctx, b.d)
	defer cancel()
	return b.p.ExecuteGrant(ctx, tenantID, grantID)
}

type boundedReader struct {
	p workflows.CaseReader
	d time.Duration
}

func (b boundedReader) ReadCaseByKey(ctx context.Context, tenantID, key string) (workflows.CaseView, error) {
	ctx, cancel := context.WithTimeout(ctx, b.d)
	defer cancel()
	return b.p.ReadCaseByKey(ctx, tenantID, key)
}

type boundedIssuer struct {
	p workflows.RouteGrantIssuer
	d time.Duration
}

func (b boundedIssuer) IssueRouteGrant(ctx context.Context, req workflows.RouteGrantRequest) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, b.d)
	defer cancel()
	return b.p.IssueRouteGrant(ctx, req)
}
