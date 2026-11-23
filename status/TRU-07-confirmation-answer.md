# Session 2026-11-19 07:55 WAT · TRU-07 (confirmation-answer.v1)

## Session summary
- What changed (PR links): a new contract `contracts/schemas/confirmation-answer.v1.json`, local commits on branch `prep/confirmation-answer-v1` (not pushed, no PR yet): a RED commit (fixtures, tests) and a GREEN commit (schema, inventory row, migration note, regenerated models, shape-refusals list).
- Why: the approver route needs a strict request body for answering a confirmation request. Founder's order, relayed (not yet a committed decision; AGENTS.md section 2 item 3): `{request_id, response: ACCEPT|DECLINE}`, additive, strict, no identity members. REQ-TRU-009, INV-25, INV-12, SoT 43.2a, AGENT-15.
- Risks, and how they are contained: (1) the body must never carry an identity. The schema is closed and has two members only; the session guard `RefuseBodyIdentity` refuses the names it lists; the closed generated type refuses every other name. Tests run both layers. (2) The Go type does not enforce the `cfr_` pattern of `request_id` (the generator does not model patterns in Go), so the route must check the shape itself or look the request up in the tenant's store. (3) The guard does not list `approver_id`, `approver_roles`, `channel` or `at`; they are refused by the closed type, not the guard (see doubts below).
- Tests added (REQ, INV, US or EC IDs): `tools/contracts/test_confirmation_answer.py` (REQ-TRU-009, INV-25, INV-12): shape, closed, both required, `request_id` is the `ConfirmationRequestId` ref, `response` is the `ConfirmationResponse` ref and no inline enum, the enum is exactly ACCEPT and DECLINE, no identity member, every invalid fixture refused for its recorded keyword. `services/core/edge/session/confirmation_answer_body_test.go` (INV-12, AGENT-10): the guard lets each schema property through, refuses identity names, the closed type refuses the names the guard does not list, a good answer decodes into `ConfirmationAnswerV1`. Fixtures: 2 valid, 34 invalid with `expectations.json`.
- Regression cases added: the invalid fixtures above.
- Debt entries: none.
- Conflicts raised: none. Doubts are listed in the hand-off report.

## What the approver route needs (for EL)
- Go type: `contracts.ConfirmationAnswerV1` (`services/core/internal/contracts/confirmation_answer_v1_gen.go`) with fields `RequestId contracts.CommonV1ConfirmationRequestId` and `Response contracts.EnumsV1ConfirmationResponse` (constants `EnumsV1ConfirmationResponseACCEPT` and `EnumsV1ConfirmationResponseDECLINE`).
- Parse: there is no separate parse function. `(*ConfirmationAnswerV1).UnmarshalJSON` is the parser: `json.NewDecoder(r.Body).Decode(&ans)` or `json.Unmarshal`. It refuses a missing member, a null member, an unknown member and a response that is not one of the two words. `ConfirmationAnswerV1Shape` is the shape it checks.
- Not checked by the Go type: the pattern of `request_id` (`^cfr_[0-9A-Za-z]{2,32}$`). Check it, or look the request up scoped to the session's tenant and answer `NOT_FOUND_OR_NOT_AUTHORIZED` for anything else.
- Mount the route behind `session.Middleware` and `session.RefuseBodyIdentity`, as the test does. Take the approver principal, roles, channel and time from the session and the clock only, and build `confirmation-event.v1` from them.
- Python: Pydantic model in `services/model/noema_model/contracts/confirmation_answer_v1.py` (generated; the model service has no use for it).
- TypeScript: not a root in `tools/codegen/ts.json`. `confirmation-event.v1` and `clarification-answer.v1` are not listed either; only `confirmation-request.v1` is (the UI reads it). If the UI will post the answer, listing it is a small follow-up (a change to ts.json and the generated UI types).

## Pre-work for the next agent
- Exact next step: push the branch and open the PR (a human decides; this session did not push), then build the approver route (TRU-07, EL).
- Open questions (owner): (1) whether `approver_id`, `approver_roles`, `channel`, `at` should also join the guard's `identityKeys` (EL/Security Reviewer; the guard is outside this change). (2) Whether the founder's order needs a committed decision record before merge (Founder).
- Files to read first: `contracts/schemas/confirmation-answer.v1.json`, `contracts/schemas/confirmation-event.v1.json`, `services/core/edge/session/bodyidentity.go`, `services/core/trust/confirm/confirm.go`.
- Do not touch: `confirmation-event.v1.json`, `confirmation-request.v1.json`, `enums.v1.json` (unchanged here).
