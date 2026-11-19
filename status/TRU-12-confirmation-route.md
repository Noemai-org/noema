# Status · TRU-12 · POST /v1/confirmations/{confirmation_request_id} (RED pushed; green local)

State: RED commit pushed with a draft PR; the green commit is local and is pushed by the Engineering Lead after reading the red job.

## Task card (short)

- Task: TRU-12, the confirmation answer route. Tier 1 (identity, Record).
- REQ: REQ-TRU-009, REQ-API-001. SoT: INV-12, INV-25, AGENT-10, AGENT-13.
- Allowlist: `services/core/cmd/core/confirmations_route.go` and its test, this file. Not touched: `compose.go`, `main.go`, `contracts/`, `trust/**`, migrations, `.github`, `deploy/`.
- Interim rulings (relayed, not yet committed): Founder decision of 19 Nov (full confirmation path; route body {request_id, response}; APPROVER_NOT_ELIGIBLE for NotEligible and WrongRequest).

## Session 2026-11-19 · TRU-12

## Session summary
- What changed: new `confirmationRoute(cfg)` (a `edge.Route`), ports `ConfirmationRequests` (Lookup) and `ConfirmationReceipts` (Receive), a decoder seam `ConfirmationAnswerDecoder`.
- Why: the approver's reply must enter through an authenticated session only (INV-12) and be recorded before anything else happens.
- Risks: see "Safer readings" in the PR body.
- Tests added: REQ-TRU-009, INV-12, INV-25, EC-U04 (see `confirmations_route_test.go`).
- Regression cases added: none.
- Debt entries: the strict decoder stands in for `confirmation-answer.v1` (waits on C1).
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: implement `ConfirmationRequests` and `ConfirmationReceipts` over the Record (the receipt is one CONFIRMATION_RECEIVED event, `confirmation-event.v1`), then the final main.go PR mounts `confirmationRoute`.
- Open questions: see the PR body.
- Files to read first: `cmd/core/confirmations_route.go`, `trust/confirm/confirm.go`, `contracts/schemas/confirmation-event.v1.json`.
- Do not touch: `trust/confirm`.
