# STM-04 wire lane: buildWorker (wire_worker.go)

State: part 1 pushed (PR noema#721); part 2 (real ports, Trust-only route issuer, drain, died-worker) red pushed, green local. Written 2026-11-18 by an agent for EL (unreviewed).

# Session 2026-11-18 · STM-04 (wire lane)

## Session summary
- What changed (PR links): noema#721: `services/core/cmd/core/wire_worker.go` and `wire_worker_test.go`. `buildWorker(ctx, env, workerPorts, dial)` reads the Temporal settings (LoadWorkerConfig of noema#676, merged), refuses a missing port or bad setting before a dial, binds one deadline (NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS, no default) to each activity port call, registers `*workflows.EscalateActivities` through `startWorker`, and returns a handle whose `Readiness()` is false (naming `worker`) when there is no worker, it did not start, or it was stopped.
- Why: REQ-VER-004 (the part that needs a worker), REQ-EXE-006, REQ-EXE-005; INV-30, INV-07, AGENT-08, AGENT-16.
- Risks, and how they are contained: no retry added (write activities keep MaximumAttempts 1, workflows/options.go); the bound is a context deadline only. Stopped is tracked by Stop() through the handle: a worker that dies on its own is not seen (the SDK seam in worker_run.go exposes no fatal-error hook).
- Tests added (REQ, INV, US or EC IDs): see wire_worker_test.go (REQ-OPS-003, REQ-EXE-006, REQ-EXE-005, INV-30, AGENT-16).
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: the lane brief says "Executor and Verifier activities over portadapters/executor and verifier"; `workflows.EscalateActivities` has Executor (GrantExecutor), Reader (CaseReader) and Issuer (RouteGrantIssuer) and no verifier activity. The real implementations are `execution/bridge.GrantExecutor` and `bridge.CaseReadBack`; no non-test RouteGrantIssuer exists. portadapters/executor and verifier implement orchestrator.Executor and Verifier (the runner's ports) and do not fit these activity interfaces. Not worked around.

## Pre-work for the next agent
- Exact next step (EL, main.go PR): build `bridge.GrantExecutor{Controller}` (needs execution pools and the grant keys, SEC-09 noema#708 open), `bridge.CaseReadBack{Case}`, and a RouteGrantIssuer over Trust; call `buildWorker(ctx, env, workerPorts{...}, dialTemporal)` with a deadline context; add `h.Readiness()` to the /readyz chain (compose.go `missing` does not know the worker); call `h.Stop()` on shutdown.
- Open questions (owner): the missing RouteGrantIssuer (Trust, TRU); the ci-profile mock-case run of acceptance criterion 6 needs these real ports and the Compose Temporal, so it is not built here.
- Files to read first: services/core/cmd/core/worker*.go, execution/bridge/orchestration.go, workflows/activities.go.
- Do not touch: workflows/options.go (D8). The workflow no-clock/no-I/O source test already exists (workflows/escalate_test.go).


# Session 2026-11-19 · STM-04 (wire lane, part 2)

## Session summary
- What changed (PR links): noema#721, `wire_worker_ports.go` (new), `wire_worker.go`, tests `wire_worker_ports_test.go`. (a) `buildEscalatePorts(env, escalateDeps)`: `bridge.GrantExecutor{Controller}` over a Controller whose grant keys come from `keyfiles.Load` (`NOEMA_GRANT_KEY_DIR`, `NOEMA_GRANT_KEY_MAX_ACTIVE`, noema#708 merged); the case read-back is supplied (`Reader`), because lint `import-adapters` forbids cmd from naming `adapters.Case`. (b) `newRouteGrantIssuer(RouteDecider)`: asks Trust (`DecideRoute`, capability `fin.cap.route_to_internal_owner`, its own decision per A335) and returns the single grant of an ALLOW; it imports no intake, signer or issuer. (c) `Stop() error`: readiness false at once, in-flight activities drained within `NOEMA_TEMPORAL_SHUTDOWN_DRAIN_MS` (no default), no activity starts after Stop, worker then stopped once; `fatalReporter` seam for a worker that dies on its own.
- Why: REQ-EXE-006, REQ-EXE-005, REQ-OPS-003, REQ-SEC-005; INV-03, INV-30, INV-07, AGENT-08, AGENT-16.
- Risks, and how they are contained: after Stop begins, an activity fails with ErrWorkerStopping before any write (the workflow stops safely, no retry). The real SDK connection does not yet implement `fatalReporter` (worker_dial.go would pass `worker.Options.OnFatalError`; outside this lane's allowlist).
- Tests added: REQ-EXE-006, REQ-EXE-005, REQ-OPS-003, REQ-SEC-005, INV-03, INV-30, AGENT-16 (see wire_worker_ports_test.go).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step (EL, main.go): supply `escalateDeps` (Controller parts over the execution pools, `bridge.CaseReadBack` over the case client, a `RouteDecider` over the Trust intake and its Planner/Recorder adapters); call `buildWorker`; add `h.Readiness()` to /readyz; call `h.Stop()` on shutdown and log ErrWorkerDrainTimeout.
- Open questions (owner): `OnFatalError` hook in worker_dial.go; the RouteDecider implementation over the intake (TRU).
- Files to read first: cmd/core/wire_worker.go, wire_worker_ports.go.
- Do not touch: workflows/options.go (D8).
