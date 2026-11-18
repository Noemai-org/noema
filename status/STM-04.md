# STM-04 · the core's Temporal worker (G1-critical)

Status: task card written 2026-11-17 by EL; updated the same day for A282 (relayed): the worker is an STM task, and the Repo Steward registers its number in `tasks.csv`. **STM-04 is the number EL proposes (STM-01 to STM-03 exist); if the Steward registers another, this file is renamed to it.** Nothing is built.

```
TASK-P7-STM-04  The core's Temporal worker: it polls one task queue, runs the EscalateCase workflow and its activities, and answers readiness
Priority: P0
Requirement IDs: REQ-EXE-006, REQ-RCV-002, REQ-EXE-005, REQ-VER-004 (the part that needs a worker, not the watch itself), REQ-OPS-003; SoT INV-30, INV-03, INV-07, INV-24, AGENT-07, AGENT-08, AGENT-16
Rationale: The escalate case (create the case, read it back by key, route it) is a Temporal workflow that exists and is tested under the Temporal test environment (`workflows/escalate.go`), but nothing runs it: no process registers it with a server. G1 needs the graph to run for real. Workflow code does no I/O and Temporal is never authoritative (INV-30); a write is never retried by Temporal (D8, REQ-EXE-006); a timeout is OUTCOME_UNKNOWN and stops the case (REQ-EXE-005).
Modules touched (allowlist): services/core/cmd/core/worker.go and worker_test.go; services/core/workflows/** (the registration tables only: no new workflow); status/STM-04.md. Protected paths (deploy/compose: the Temporal address and TLS settings for the core service) are IL's, in a separate PR.
Contracts: none; bump required? N
Inputs/outputs: in: settings with NO default (AGENT-16): NOEMA_TEMPORAL_ADDRESS, NOEMA_TEMPORAL_NAMESPACE, NOEMA_TEMPORAL_TASK_QUEUE, the client certificate, key and CA files (mTLS, PERD 36.1), the activity timeout (REQ-OPS-003); the real ports of `EscalateActivities` (Executor, Reader, Issuer) from the composition root. out: a worker that polls the queue; `Components.Worker`; readiness that names the first missing part.
Acceptance criteria: (1) the worker starts only with every setting present and refuses to start with a missing, malformed or plain-text Temporal setting, naming it; (2) it registers exactly the workflows and activities the `workflows` tables declare, and the options it applies come from `workflows/options.go` (write activities MaximumAttempts 1 and no backoff, read activities bounded: D8); (3) a write activity that returns an unknown outcome stops the workflow with a reason and nothing is retried by Temporal (REQ-EXE-005, REQ-EXE-006); (4) workflow code reads no wall clock and does no I/O (a source test over `workflows/*.go`, INV-30); (5) no credential for the core database, the adapters or the grant keys reaches the worker's Temporal client, and the worker never reads the `temporal` database (INV-29, INV-30); (6) the escalate workflow runs end to end under a real worker against the Compose Temporal in the ci profile with the mock-case target, and its Record events are asserted.
Tests to write FIRST (AGENT-11): worker_test.go: a table of refused configurations (each setting missing, an address with no port, a plain-text setting, a zero timeout); the registration set equals the declared set (a workflow added without a table row fails); the applied options equal D8; the source test for (4); the Record events of the escalate run (INVESTIGATION_STATE_CHANGED, GRANT_SPENT, DISPATCH_ATTEMPTED, RECEIPT) are asserted in the ci-profile run.
Faults to exercise (AGENT-12): Tushe or mock-case FaultPlans for a timeout and a 5xx on the create write (the case stops with OUTCOME_UNKNOWN and no second write), a worker restart mid-workflow (the run resumes from history and spends no second grant: replay determinism, INV-21), and Temporal unavailable at start (the core serves health and /readyz says why; it does not crash-loop silently).
Dependencies: PLT-04 (Compose Temporal; done), the composition root (noema#614, merged), the Execution and Outcome adapters of the escalate activities (noema#576 and C1's), TEN-05 (the simulated clock follows Temporal test time: noema#657).
Out of scope: Mode 2; the durable outcome watch workflow itself (REQ-VER-004 is its own task); schedulers; any new dependency (the SDK is approved, DEC-2026-10-25); changing a retry budget, timeout or threshold (AGENT-16).
Definition of done: tests green; boundary/lint green; Record events asserted; docs updated; PR answers the SoT §52 questions
If blocked or conflicting: STOP and emit the CONFLICT block (§71.3)
```

## Open questions (owner)
- Task queue name and namespace: deployment data (IL), not code.
- Whether one worker serves both Execution and Outcome activities in the POC or two task queues are wanted: smallest safe default is one queue, one process (the core), because the model service must hold no Temporal client (INV-29).

## Order of work
1. Settings loader with refusals (tests first). 2. Registration from the declared tables and the D8 options. 3. The source test (no clock, no I/O in workflow code). 4. The worker in the composition root, readiness. 5. The ci-profile run with mock-case (needs the real activity ports from #576 and C1's adapters).
