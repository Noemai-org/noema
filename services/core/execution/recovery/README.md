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

## Not here

- The recovery decision table itself (PERD 30.2) is Trust policy data (`fin.pol.recovery`), not code in this package.
- The Temporal recovery workflow that calls `Handle`, the budget of two dispatches per logical key (RCV-02), verify-by-key (Outcome, OUT-01) and compensation (RCV-04).

## Tests

`go test ./execution/recovery/` (no database). Each test names REQ-RCV-001; a `rapid` property builds Record sequences with retries that are ordered, missing a verdict, missing an ALLOW or in the wrong order, and requires the checker to flag exactly the broken ones with the right reason.
