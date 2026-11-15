# execution/recovery

Recovery after an uncertain or failed write. Built by RCV-01 (slice 1, 2026-11-05).

**Implements:** REQ-RCV-001 (recovery only through Trust). SoT IDs: INV-15, INV-24, AGENT-08.

## What it does

- `Handler.Handle(ctx, req)` takes a RecoveryRequest (`recovery-request.v1` fields: a verdict ID or a voided-grant ID, never both) and asks Trust, through the `Trust` port, to evaluate it afresh. The package imports neither Trust nor Outcome and has no method that dispatches.
  - Trust's `ALLOW` with a decision ID gives `RETRY_AUTHORISED` carrying that decision. The caller still needs a new grant (REQ-EXE-006).
  - A Trust error, a panic, `UNKNOWN`, an outcome that is not one of the seven, or an `ALLOW` with no decision is UNKNOWN: `ESCALATE` and an ops alert (PERD 30.2, REQ-RCV-001 failure behaviour). If the alert cannot be delivered the result is still `ESCALATE` and the failure is returned.
  - Any other Trust outcome (`ESCALATE`, `REJECT`, `CLARIFY`, `CONFIRM`, `WAIT`) gives `ESCALATE` with no alert from here, because Trust recorded why.
  - The context needs a deadline; a malformed request is refused before Trust is asked.
- `CheckRetryOrder(rows)` checks, over rows already read and verified, that every `DISPATCH_ATTEMPTED` with `attempt` > 1 is preceded, since the previous dispatch, by a `VERDICT` other than `ACHIEVED` and then by a `DECISION_MADE` with the outcome `ALLOW`. Each retry needs its own pair. A dispatch whose attempt cannot be read, and a verdict that cannot be read, are never taken as safe. The verdict is read from the payload Outcome writes (the verdict document nested under `verdict`, the enum at `verdict.verdict`); the flat `{"verdict": "<enum>"}` that the reconstruction projection reads is accepted too.

## The budget (RCV-02)

**Implements:** REQ-RCV-003 (two dispatches per logical key, then escalate), REQ-RCV-002. SoT IDs: INV-07, AGENT-08, AGENT-16.

- `NewGate(max)` takes the budget from pack data (`execution/limits`, `execution-limits.v1`; ruling A3: 2). There is no default; below 1 is refused.
- `Gate.Decide(tenantID, rows, key)` counts `DISPATCH_ATTEMPTED` events of the key `tenant_id`, `correlation_id`, `capability`, `target_ref` in the verified rows of one tenant's chain and answers `DISPATCH_ALLOWED` or `BUDGET_SPENT` (the case escalates). A dispatch of the key's correlation whose capability or target cannot be read counts against every key of that correlation. A key of another tenant, or with an empty part, is refused.
- `CheckBudget(tenantID, rows, max)` flags every dispatch beyond the budget (`BUDGET_EXCEEDED`) and every dispatch with no readable key (`KEY_UNREADABLE`).
- `DISPATCH_ATTEMPTED` now carries `capability` and `target_ref` (written by `execution/ledger` Spend from the grant), so the key can be read from the Record.

## Not here

- Enforcement inside Spend (slice 2, a decision is open: see `status/RCV-02.md`).

- The recovery decision table itself (PERD 30.2) is Trust policy data (`fin.pol.recovery`), not code in this package.
- The Temporal recovery workflow that calls `Handle`, verify-by-key (Outcome, OUT-01) and compensation (RCV-04).

## Tests

`go test ./execution/recovery/` (no database). Each test names REQ-RCV-001; a `rapid` property builds Record sequences with retries that are ordered, missing a verdict, missing an ALLOW or in the wrong order, and requires the checker to flag exactly the broken ones with the right reason.

**COMPENSATED (ruling A68 of `DEC-2026-11-08-rulings-a41-onward`, merged; called A6 in the 8 Nov answers to C1).** A `COMPENSATED` verdict does not authorise a retry. `CheckRetryOrder` treats it as a recognised verdict that authorises nothing and that ends the authority of any earlier failed verdict. A Trust `CLARIFY` or `CONFIRM` on a recovery escalates, as before.

