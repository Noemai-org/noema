# STM-01 budget-spent re-evaluates

State: In review (green commit local until EL pushes it).

# Session 2026-11-19 STM-01

## Session summary
- What changed: `ReceiptBudgetSpent` takes the existing arrow PLANNED to EVALUATING and asks Trust again (docs#206 option b, relayed, not yet committed). Bounded by `u.voids` against `MaxDispatchesPerKey`; at the limit a further ALLOW stops with `ErrRecoveryBudgetSpent`.
- Why: Trust's T12 decides ESCALATE DISPATCH_BUDGET_EXHAUSTED (noema#572); the run previously stayed in PLANNED.
- Risks: a Trust defect that keeps allowing at the limit is stopped by the runner backstop.
- Tests added (REQ-RCV-003, INV-03, AGENT-08): three in runwrite_test.go; the test that pinned the old stop is replaced by them.
- Regression cases added: the loop-guard test.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: Codex and SR at the same head (Tier 1).
- Open questions: none.
- Files to read first: services/core/orchestrator/runwrite.go.
- Do not touch: trust/**, the table.
