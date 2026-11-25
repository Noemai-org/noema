# STM-05 · the recovery reader (A696)

# Session 2026-11-25 · STM-05 (C1: the Postgres reader of the recovery pass)

## Session summary
- What changed (PR links): noema#996. `trustpg.Decisions.AllowOf` (the newest ALLOW of a correlation, its grants and intent reference, as the tenant's `trust_rw` login) and the new package `orchestrator/portadapters/recoverypg` (`Reader.Idle`: the tenant's idle rows in the configured states, oldest first, as its `orchestrator_rw` login, on the database's clock). Red `1d2719a2` (stubs), then green.
- Why: A696 gives C1 the stack-check gaps "the recovery reader" and "the recovery write path" (the latter in its own PR, below). `recovery.Reader` has no implementation on main: the pass lists nothing, so `/readyz` names `recovery: reader not built`.
- Risks, and how they are contained: the reader only reads (a read-only transaction, no statement that changes a row, pinned by a source scan); each read is the tenant's own login under forced row-level security and the tenant is also in every WHERE; idleness is the database's clock; the batch bound and the idle age are arguments from `recovery.Config` (no code default, AGENT-16); a read that fails is an error with fixed text, never an empty list.
- Tests added (REQ, INV, US or EC IDs): REQ-STM-001, REQ-RCV-003, REQ-SEC-004; INV-03, INV-12 (see `recoverypg/reader_test.go`, `trustpg/decisions_test.go`).
- Regression cases added: none beyond the tests above.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL wires `recoverypg.New(orchestratorPools, trustpg.NewDecisions(trustPools, t), states, t)` into `SurfaceSeams.Recovery`; the recovery write path (its own PR) names what is still missing.
- Open questions ([OPEN], for the Architect and EL):
  1. Which states a pass takes up. `New` requires the list (no default). A row resting on a person or a third party (AWAITING_CLARIFICATION, AWAITING_CONFIRMATION, WAITING, DORMANT, AWAITING_EXTERNAL_OUTCOME) is idle by design: listing it makes the pass count it NEEDS_PERSON at every restart and, ordered oldest first within a batch, can starve a real crash. Smallest safe default: the three states `ResumeWrite` resumes (PLANNED, EXECUTING, AWAITING_VERIFICATION). Evidence that would invalidate it: a ruling that other in-flight states (RECEIVED, INTERPRETING, EVALUATING) must also be reported by the pass.
  2. A correlation with several ALLOW decisions (an escalation chain gives one per step): the newest is read; Bindings prove it against the Record before anything moves, and a grant the Record shows as spent is only verified.
- The recovery write path (the second `/readyz` gap of A696) is not a separate piece of code: it is the write seam (`orchestrator.WritePorts`: Journal, Verifier, Bindings, with `recovery.NoDispatch` as the Executor, which `recoveryWritePorts` already substitutes). It is blocked on parts not on main: the Verifier needs slice 4's request reader (EL), which waits on `step_params` in the signed receipt (A692, C1's contract PR), and the Executor side is noema#976. Nothing here guesses them.
- Files to read first: `orchestrator/recovery/recover.go`, `orchestrator/portadapters/recoverypg/reader.go`.
- Do not touch: `cmd/core/compose*.go` (EL's composition).
