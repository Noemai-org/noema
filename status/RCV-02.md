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
