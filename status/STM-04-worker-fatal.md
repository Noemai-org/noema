# STM-04-worker-fatal · the worker's died-on-its-own hook

State: red pushed (draft PR #794); green committed locally, not pushed (EL pushes after reading the red job). Not Done.

# Session 2026-11-19 WAT · STM-04

## Session summary
- What changed (PR links): noema#794. `worker_dial.go`: `fatalState` (channel of one, sync.Once, atomic flag, no goroutine) filled by `worker.Options.OnFatalError`; `sdkConnection` exposes `Fatal() <-chan error` and `Ready() bool`. Tests in `worker_fatal_test.go`.
- Why: a worker that dies on its own must be visible to the process (REQ-OPS-003, AGENT-07). Note the hook is `worker.Options.OnFatalError`, not a client option.
- Risks, and how they are contained: Fatal is not yet forwarded by `RunningWorker` (worker_run.go, outside the lane allowlist) nor read by main or readiness. Until then nothing acts on it.
- Tests added (REQ, INV, US or EC IDs): five tests, REQ-OPS-003, INV-30. Hand mutation: 5 of 5 killed (one is a compile error).
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: in `worker_run.go`, add `Fatal() <-chan error` and `Ready()` on `RunningWorker`, forwarding through an optional interface on the `Connection` (`interface{ Fatal() <-chan error; Ready() bool }`); then main selects on it and readiness reports it.
- Open questions (owner): whether the process exits or only fails readiness on fatal (EL).
- Files to read first: worker_dial.go, worker_run.go.
- Do not touch: workflows/**.
