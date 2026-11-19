# TRU-12-plangraphs

State: PR noema#762. Session 2 (six param kinds, runnable-only): red pushed, green held by EL.

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


# Session 2026-11-19 (2) · TRU-12

## Session summary
- What changed (PR links): noema#762, adapter reworked to six param kinds (slot, const, map, fact, policy_param, evidence_packet; DEFAULT removed), `PlanContext` (Runnable, PolicyParams, Facts, EvidencePacket) through the `PlanContexts` port, runnable-only planning, floats refused (SR condition 2).
- Why: the Founder's text of 19 Nov 2026 (interim, relayed, not yet committed) for noema#765.
- Risks, and how they are contained: safer readings: a runnable id the graph lacks, or none, is refused; an empty fact, policy param or packet counts as absent; a map with no value or an empty code is a bad declaration.
- Tests added (REQ, INV, US or EC IDs): REQ-TRU-009, INV-02, INV-18, AGENT-09, AGENT-13.
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the continuation lane implements `PlanContexts` over the PlanRequest fields; the composition fills `Pack` from noema#765's `params`.
- Open questions (owner): SR condition 1 (expected kind per slot param) needs a contract member (Architect).
- Files to read first: `plangraphs/types.go`, `plangraphs/adapter.go`.
- Do not touch: `trust/planner.go`, `trust/intake/**`.
