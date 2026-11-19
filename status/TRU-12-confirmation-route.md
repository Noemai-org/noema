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

# Session 2026-11-19 (A341 rework) · TRU-12

## Session summary
- What changed: the route now follows A341 (interim, relayed, not yet committed). The store owns CONFIRMATION_RECEIVED: the route calls one `ConfirmationAnswers.Answer(ctx, ConfirmationAnswerInput)` (shape of `trustpg2.Requests.Answer`, noema#756) and appends nothing. First answer wins: a second answer is 409 CONTRADICTORY_STATE; the identical first answer by the same approver returns the stored result (202). The default decoder is the generated `contracts.ConfirmationAnswerV1` (on main). A `Timeout` bounds the store calls (no default; the wiring supplies it).
- Why: A341, INV-12, REQ-TRU-009.
- Risks: the interface is consumer-defined with its own input type and `ErrConfirmationAnswered`; the wiring adapter (final main.go) maps `trustpg2.AnswerInput` and `ErrRequestConflict` to them.
- Tests added: first answer wins (3 cases), replay, ineligible never reaches the store, bounded context, generated decoder (REQ-TRU-009, INV-12, INV-25, EC-U04). Mutation: 10 of 10 killed.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: an adapter from `trustpg2.Requests` to `ConfirmationAnswers` and `ConfirmationRequests` (Lookup), and the timeout setting, in the final wiring.
- Open questions: refused replies are not recorded (the store's Answer records accepted ones only); the 409 code for a second answer.
- Do not touch: `trust/confirm`.
