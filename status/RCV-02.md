# Status · RCV-02 (Core Engineer 1) · recovery budget

Updated 2026-11-06. Plan task RCV-02: owner C1+AG; REQ-RCV-003, REQ-RCV-002; Needs RCV-01 (noema#175, in review); done when "US-16 passes; property test: no third dispatch ever".

## State: slice 1 in review (STACKED on noema#175). RCV-02 is not Done.

## Session 2026-11-06 · RCV-02

### Session summary
- What changed (PR links): the RCV-02 pull request on `feat/RCV-02-budget`: `execution/recovery` `Gate` (`NewGate`, `Decide`), `CheckBudget`; `execution/ledger` Spend writes `capability` and `target_ref` into `DISPATCH_ATTEMPTED`.
- Why: REQ-RCV-003 and ruling A3: budget 2 per `tenant_id`+`correlation_id`+`capability`+`target_ref`, counted over `DISPATCH_ATTEMPTED` events. The Record did not carry the capability or the target in that event, so the key could not be read from it.
- Risks, and how they are contained: a budget that undercounts allows a third dispatch. An unreadable key counts against every key of its correlation; a key of another tenant is refused; the budget has no default. The payload change adds two fields to an event; no reader of the event breaks (record, reconstruct, outcome_e2e tests pass).
- Tests added (REQ-RCV-003): the third dispatch is refused with a budget of 2; other correlations, capabilities and targets do not share a budget; an unattributable dispatch counts conservatively; only `DISPATCH_ATTEMPTED` counts; `CheckBudget` flags exactly the dispatches beyond the budget; a `rapid` property, no third dispatch ever for any interleaving of keys; a key of another tenant is refused; the ledger test that the event carries the key. Mutations run and reverted (all caught): off by one in the gate; unreadable dispatch not counted; correlation ignored; off by one in the checker; tenant check removed.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: slice 2, enforce the budget inside Spend (E8) so a third dispatch is impossible at the database, not only refused by a caller that asks. Open decision (Architect): count in the Spend transaction from the Record (needs a read privilege for `execution_rw` on the Record) or keep a counter on the A3 key in `idempotency_record` (needs a migration and a column). My recommendation: the counter, in the same transaction as the spend, because Execution's login must not read the Record; the Record stays the proof (`CheckBudget`).
- Also: US-16 (the end-to-end story) needs the Temporal recovery workflow; not in this slice.
- Open questions (owner): the enforcement location above (Architect).
- Files to read first: `services/core/execution/recovery/README.md`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).

## Session 2026-11-12 14:00 WAT · slice 2: the budget is enforced by the database (ruling A153)
### Session summary
- What changed (PR links): the RCV-02 slice 2 pull request on `feat/RCV-02-budget-in-db`, from main: migration `0023_dispatch_budget.sql` (`dispatch_budget_count`, `grant_spend_budget_guard`, the trigger `grant_spend_budget`); `Ledger.Spend` maps the new SQLSTATE `NS002` to `SpendResult.BudgetSpent`; `controller.Outcome.BudgetSpent`.
- Why: ruling A153 (DEC-2026-11-12-architect-rulings-a148-a160, relayed until docs#157 merges): RCV-02 enforcement lives in the database; key = tenant, correlation, capability, target; counting `DISPATCH_ATTEMPTED`; C1, migration 0023. Slice 1 (noema#196) gave the gate in Go; this is the open decision of that slice's note, decided.
- Design: the guard is on the insert into `grant_spend`, not on the `DISPATCH_ATTEMPTED` insert. A first version refused any `DISPATCH_ATTEMPTED` event with no readable key; it broke 14 tests of `record/chain` and `record/reconstruct` (property tests and golden fixtures that append key-less events) and was dropped. What must be impossible is a third spend, and that is what the guard refuses.
- Ceiling: the database holds 2 (A3; A62: the pack may lower it, never raise it above 2). A lower pack value is the Go gate's.
- Risks, and how they are contained: a miscounted key lets a third dispatch through. Seven mutants of the migration (budget 3, no tenant filter, no correlation filter, no target filter, no capability filter, unreadable events not counted, unreadable grant allowed, off by one) are each killed by a named test. The merge of this branch with the open migration PRs #261 (0018), #263 (0019), #268 (0020) and #272 (0021) was run in a scratch tree: no conflict, and `platform/db` and `execution/ledger` pass.
- Tests added (REQ-RCV-003, INV-03, AGENT-08, A3, A62, A153): `execution/ledger/budget_test.go` (7): the third spend refused with nothing written and the grant still ISSUED, each key's own budget, only the key's own dispatches count (another target, correlation, capability, tenant), unreadable earlier dispatches count against the correlation, an unreadable grant is refused, a direct insert into `grant_spend` is refused, 8 concurrent Executes never dispatch more than twice.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.
- Not done, and why: no void reason and no event for the refusal. GRANT_VOIDED reasons and SECURITY_EVENT kinds are closed lists of `record-event.v3`, so `BUDGET_SPENT` would be a contract change (AGENT-15). The grant stays ISSUED (it expires), `Outcome.BudgetSpent` tells the caller to escalate, and the database refuses the same grant again each time. The Architect decides whether to add the reason and a kind.
- Not done: US-16 (the end-to-end recovery story) needs the Temporal recovery workflow.

### Pre-work for the next agent
- Exact next step: the caller of `Execute` escalates on `Outcome.BudgetSpent` (the workflow of RCV-02/RCV-03); the Architect's decision on a `BUDGET_SPENT` void reason and a `SECURITY_EVENT` kind (a `record-event` contract version, AGENT-15).
- Open questions (Architect): the void reason and event kind above; migration number 0023 waits for 0022 (A151, noema#410) in the A113 order.
- Files to read first: `services/core/migrations/0023_dispatch_budget.sql`, `services/core/execution/ledger/spend.go`.
- Do not touch: `migrations/README.md` (A145).

## Session 2026-11-12 16:00 WAT · RCV-02: the migration number and the void reason are settled
### Session summary
- What changed (PR links): noema#472: the header comment of `0023_dispatch_budget.sql` (no statement changed) and this note.
- Why: ruling A167 (docs#159) made C1's table on noema#183 the migration ledger: 0023 stays with this PR and does not wait for 0022, which the table reserves for the evidence-set binding (A164 already said the budget does not wait). The void reason: A164 (no new event kind, no new void reason) applies, which the code already follows (a refused dispatch leaves the grant ISSUED, `BudgetSpent` is a result, not a void).
- Risks: none; a comment-only change in an unmerged migration.
- Tests added: none. Regression cases: none. Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the Security Reviewer re-reads `218edc95`'s successor for the two points of its NOT READY (migration order, void reason); both are answered by A167 and A164.
- Do not touch: the trigger and the definer function.
