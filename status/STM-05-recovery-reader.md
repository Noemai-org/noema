# STM-05 · the recovery reader (A696)

# Session 2026-11-25 · STM-05 (C1: the Postgres reader of the recovery pass)

## Session summary
- What changed (PR links): see the PR of branch `feat/STM-01-recovery-reader`. Red: `trustpg.Decisions.AllowOf` and the new package `orchestrator/portadapters/recoverypg` (`Reader.Idle`), tests and stubs that answer nothing.
- Why: A696 gives C1 the stack-check gaps "the recovery reader" and "the recovery write path" (the latter in its own PR, below). `recovery.Reader` has no implementation on main: the pass lists nothing, so `/readyz` names `recovery: reader not built`.
- Risks, and how they are contained: the reader only reads (a read-only transaction, no statement that changes a row, pinned by a source scan); each read is the tenant's own login under forced row-level security and the tenant is also in every WHERE; idleness is the database's clock; the batch bound and the idle age are arguments from `recovery.Config` (no code default, AGENT-16); a read that fails is an error with fixed text, never an empty list.
- Tests added (REQ, INV, US or EC IDs): REQ-STM-001, REQ-RCV-003, REQ-SEC-004; INV-03, INV-12 (see `recoverypg/reader_test.go`, `trustpg/decisions_test.go`).
- Regression cases added: none beyond the tests above.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL wires `recoverypg.New(orchestratorPools, trustpg.NewDecisions(trustPools, t), states, t)` into `SurfaceSeams.Recovery`; the recovery write path (its own PR) names what is still missing.
- Open questions: see the [OPEN] section of the PR body.
- Files to read first: `orchestrator/recovery/recover.go`, `orchestrator/portadapters/recoverypg/reader.go`.
- Do not touch: `cmd/core/compose*.go` (EL's composition).
