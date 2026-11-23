# Session 2026-11-22 05:40 WAT · TRU-14 (C1, ruling A511: the grammar of `applies_when`)

## Session summary
- What changed (PR links): a test only. `trust/intakeadapters/applies_when_test.go` pins that every action graph of the shipped pack has an `applies_when` that parses and type-checks as a boolean predicate with the Trust policy expression evaluator (`internal/predicate`) under the pack's own vocabulary (the scope the policy's conditions are checked with).
- Why: A511 (interim, `DEC-2026-11-21-architect-rulings-a507-a517`): the `applies_when` evaluator reuses the Trust policy expression evaluator, with no new language; if the grammar differs, C1 raises a CONFLICT block. **Result: the grammar does not differ.** The three graphs' forms (`true`, `case_id != null`, `subject_resource.status == 'RESOLVED'`) compile under the same package and vocabulary as the policy's own conditions (`exists(case where ...)`, `case_id != null`). `action-graph`'s `applies_when` and the policy rule's `condition` are the same `common.v1` Predicate type. No CONFLICT block.
- Risks, and how they are contained: none (a test). One hand mutant killed and restored: a graph with `case_id ~= null` fails the test with the evaluator's SYNTAX error, which is the signal to raise a CONFLICT (A511), not to add a language.
- Tests added (REQ-TRU-004, REQ-PLN-002; INV-14): the one above. Regression cases added: the same. Debt entries: none. Conflicts raised: none.
- **The other half of A511 (extending `intake.PlanRequest`) is not started, on purpose:** noema#785 already adds `Capabilities` (the capability IDs to plan now, which is `plangraphs.PlanContext.Runnable`), `PolicyParams`, `Facts` and `EvidencePacket` to `PlanRequest`, filled by the continuation evaluation from T11's `PermitParams`; a second PR on the same struct would conflict and would be stacked on it. What is left after #785 merges is to fill the same fields for the FIRST evaluation from Trust's own steps (the authorised-and-runnable set and T11's permit params), which #785's first evaluation leaves empty.

## Pre-work for the next agent
- Exact next step: after noema#785 merges, C1 fills `PlanRequest.Capabilities` and `PolicyParams` for the first evaluation (T10 to T12), test first; EL builds `plangraphs.Intents` and `PlanContexts` (A511).
- Files to read first: `trust/intake/ports_t13_t15.go` (`PlanRequest`, on #785's branch), `orchestrator/portadapters/plangraphs/types.go` (`PlanContext`), `internal/predicate`.
- Do not touch: `packs/fintech/graph/*.json` (A510: C2 hash-pins the action graphs).
