# TRU-12-plangraphs

State: PR noema#762 (draft). Red commit pushed; green commit held by EL.

# Session 2026-11-19 · TRU-12

## Session summary
- What changed (PR links): noema#762, new package `services/core/orchestrator/portadapters/plangraphs` (the `trustport.PlanGraphs` adapter), coded against main at 8e2f8e89 (noema#751 merged).
- Why: founder's order of 19 Nov (A341 to A357, interim, relayed, not yet committed), item 3: step params only from slots and pack data (INV-02).
- Risks, and how they are contained: the pack has no param declaration, so `ParamSpec` is consumer-side data (OPEN: a contract change). Safer readings are listed in the PR body.
- Tests added (REQ, INV, US or EC IDs): REQ-TRU-009, INV-02, INV-18, AGENT-09.
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the composition (wire_trust) supplies `Pack` (from the loaded pack) and `Intents` (slots and target from the intent store), and the pack schema gains a param declaration.
- Open questions (owner): the pack param declaration shape (Architect, EL).
- Files to read first: `plangraphs/types.go`, `trust/planner.go`.
- Do not touch: `trust/planner.go`, `trust/intake/**`.
