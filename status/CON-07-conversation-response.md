# Session 2026-11-20 · CON-07 · conversation-response.v1 (noema#807; rulings A234 and A446, interim, relayed, not yet committed)

## Session summary
- What changed (PR https://github.com/Noemai-org/noema/pull/815): new `contracts/schemas/conversation-response.v1.json`, its fixtures (3 valid, 30 invalid), the INVENTORY row, the migration note in `contracts/README.md`, a root in the `customer` list of `tools/codegen/ts.json`, the regenerated Go, Pydantic and TypeScript models, registries and `shape_refusals.json` (by the tools), `tools/contracts/test_conversation_response.py`, `services/core/cmd/core/conversations_contract_test.go`, and the customer parser lists of `tools/codegen/test_codegen.py` and `ui/test/contracts.test.mjs`. The route (`conversations.go`) is not edited.
- Why: the Frontend Engineer needs the success answer of `POST /v1/conversations` as a public contract (noema#807). The answer was already written per A234; nothing published its shape. Code and A234 agree (five members; `state` `understanding`), so no conflict.
- Risks, and how they are contained: PERD 39.1 [REC] says bodies carry `schema_version`; A234 and the handler have none (a later major can add it). `state` is a constant, not the six-value enum (loosening is additive). The ULID first character is 0 to 7 as the Record guard's ID rule; the handler's own check is wider. Safer readings are listed in the PR body.
- Tests added (REQ-API-001, REQ-UI-001, REQ-SEC-003, INV-12): `test_conversation_response.py`; the fixtures through `validate.py`; the Go test of the real handler answers (202 and 200, real ID minter, 64 inputs) with a negative test; the Pydantic and UI fixture loops. Red first at `21c8af60` (PR 815).
- Regression cases added: none (new contract).
- Debt entries: none. Known gap, as CON-08: Go has no schema validator; the Go test evaluates the patterns itself.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL pushes the green commit, reads the run, asks for Codex. The Frontend Engineer imports `parseConversationResponseV1` from `ui/src/contract_types/customer.ts`.
- Open questions (Architect): `schema_version` on this body (PERD 39.1 [REC] against A234's list); the constant `state`.
- Files to read first: `contracts/schemas/conversation-response.v1.json`, `services/core/cmd/core/conversations_contract_test.go`.
- Do not touch: `services/core/cmd/core/conversations.go` (EL's route).

# Session 2026-11-21 15:20 WAT · CON-07 (C1, the cross-member relations, A479)

## Session summary
- What changed (PR links): noema#873: `tools/contracts/validate.py` gains `conversation_response_mismatches` and the expectation `conversation_id_mismatch`; four invalid fixtures (the schema alone accepts each); the core's writer (`cmd/core/conversations.go`) gains `checkAnswer`, run on the derived answer before anything is written, with the derivation behind a seam (`startConversationWith`) so a test can feed an inconsistent derivation through the real handler. The real route is unchanged.
- Why: A479 (interim, relayed): the follow-up of noema#815. A schema states each member alone; investigation_id equal to conversation_ref, a stream_url that names it and a correlation_id of the same ULID are relations it cannot state, so no reader may take shape validation as proof of consistency.
- Risks, and how they are contained: a new refusal only, the envelope of every other internal failure, no member or value in its text (AGENT-13). No schema or contract version change (AGENT-15 not triggered).
- Tests added (REQ-API-001, REQ-UI-001, REQ-SEC-003; INV-12): `test_conversation_response.py` `RelationCheck` (7 tests); `conversations_relations_test.go` (5 tests, reading the same fixtures). Four hand mutants killed and restored (the stream_url comparison dropped in the validator and in the writer, the ULID comparison weakened, the writer's check not called).
- Regression cases added: the four fixtures. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: FE's part of A479 (the UI refuses an inconsistent answer before polling) uses the same three relations; `contracts/fixtures/invalid/conversation-response/*mismatch*` are the cases.
- Open questions (owner): none.
- Files to read first: `tools/contracts/validate.py` (`conversation_response_mismatches`), `services/core/cmd/core/conversations.go` (`checkAnswer`).
- Do not touch: `contracts/schemas/conversation-response.v1.json` (the relations are checked beside it).
