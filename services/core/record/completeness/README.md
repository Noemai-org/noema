# record/completeness

The completeness check per consequential decision. Built by REC-04 (slice 1, 2026-11-05).

**Implements:** REQ-AUD-002 (every governed write has the full event set; missing events are counted in `noema_audit_incomplete_total`). SoT IDs: INV-10, SoT §38.2.

## What it does

`Check(rows, extra)` judges each correlation in rows that the caller has already read and verified (`chain.ReadChain`, `chain.VerifyRows`). It reads no database.

- A correlation is a **governed write** when it holds any of `GRANT_ISSUED`, `REVALIDATION_PASSED`, `GRANT_SPENT`, `DISPATCH_ATTEMPTED` or `RECEIPT`, **or** a `DECISION_MADE` whose outcome is `ALLOW`, or whose outcome cannot be read (unknown is treated as consequential, so a damaged row cannot hide a write).
- A governed write must hold `DECISION_MADE`, `PLAN_CREATED`, `GRANT_ISSUED`, `REVALIDATION_PASSED`, `GRANT_SPENT`, `DISPATCH_ATTEMPTED`, `RECEIPT`, at least one `VERIFICATION_READ`, `VERDICT`, and either `EXPLANATION_RENDERED` or `EXPLANATION_BLOCKED` (reported as `EXPLANATION_RENDERED|EXPLANATION_BLOCKED`).
- `extra` maps a correlation to the event types a benchmark case lists in `required_audit_events`; those are required whether or not the correlation is a governed write, so a clarification case is judged by its own list.
- `CheckWith(rows, extra, metrics)` calls `AuditIncomplete()` once per incomplete correlation (`noema_audit_incomplete_total`, no label).

## What it does not do

- It does not decide that a correlation exists when no event or case names it; a whole path erased from the Record is the head check's and the anchoring's job (`record/verify`, REC-07).
- It does not check event order or content; the state machine and the reconstruction do.
- Not yet wired into the verification run, the benchmark audit or the reconstruction endpoint (later slices).

## Tests

`go test ./record/completeness/` (no database). Each test names REQ-AUD-002; a `rapid` property drops any one required event among extras in any order.
