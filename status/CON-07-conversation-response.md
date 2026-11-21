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
