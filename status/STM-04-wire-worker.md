# STM-04 wire lane: buildWorker (wire_worker.go)

State: red pushed (PR noema#721, draft); implementation follows the red run. Written 2026-11-18 by an agent for EL (unreviewed).

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
