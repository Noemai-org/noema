# STM-01-runnable

State: red pushed (noema#782), green committed locally, not pushed.

# Session 2026-11-19 WAT · STM-01 (runnable steps)

## Session summary
- What changed (PR links): new package `services/core/orchestrator/portadapters/runnable` (noema#782).
- Why: A363 (interim, relayed, not yet committed): `Authorized` must be the runnable steps, not every capability rooted at the request.
- Risks, and how they are contained: a dependency counts only if runnable now (safer reading); missing facts are unknown. 16 hand mutants killed.
- Tests added (REQ, INV, US or EC IDs): REQ-AG-001, INV-03.
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: C1 wires `runnable.Runnable` into `trustpack.Authorized` (see the PR report).
- Open questions (owner): edge `condition` syntax is assumed to be a predicate (EL).
- Files to read first: `services/core/orchestrator/portadapters/runnable/runnable.go`.
- Do not touch: `trustpack` from this PR.
