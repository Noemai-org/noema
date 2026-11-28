# Status · STM-01 · compose Planner (A684 step 2)

State: reds `586667a59` and `fe2a27f48`, greens committed locally; not pushed.

# Session 2026-11-26 · STM-01

## Session summary
- What changed (PR links): none yet. `trustpack/nodeplans.go`: the read-only accessor `Pack.NodePlans()` (A551). `cmd/core/compose_planner.go`: `buildPlanner`, `packPlanGraphs` (plangraphs.Pack), `storedSlots` (plangraphs.Intents), `poolDocuments` (the stored document as the tenant). `wire_decisions.go`: the Planner is built in the decisions lane; the stale gap texts `needGraphReader` and `needIntentSlots` are gone.
- Why: A684 step 2 (flip the Planner gap) with A454 (the node's slot is the single authority of a slot's kind) and A551 (accessor rule).
- Risks, and how they are contained: the Planner is still a named gap, because plangraphs.Adapter.MaxTextBytes has no value in any ruling or setting (A330); the gap names only that. Quantity, Party and TimeRef slots are never offered as resolved (no ruling states their param shape). A param with no source or two, an undeclared slot and a constant that is not plain data are left for the planner to refuse.
- Tests added (REQ-STM-001, REQ-TRU-009; INV-02, INV-14, INV-16; AGENT-09, AGENT-13): `trustpack/nodeplans_test.go`, `cmd/core/compose_planner_test.go`. Hand mutants: see the PR body.
- Regression cases added: the same tests.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: a ruling that names the text bound (and its setting, proposed: `NOEMA_PLAN_TEXT_MAX_BYTES`, no default); then set `decisionInputs.PlanTextMaxBytes` from it in the main composition and the Planner gap closes.
- Open questions (owner): the plan-param shape of Quantity, Party and TimeRef slots (Architect).
- Files to read first: `cmd/core/compose_planner.go`, `trustpack/nodeplans.go`.
- Do not touch: `plangraphs`, `trust/**`.
