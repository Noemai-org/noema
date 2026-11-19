# Session 2026-11-18 · CON-08 · message-request.v1 (order of 18 Nov "A336: message-request.v1", relayed, not committed; noema#656, ruling A275)

## Session summary
- What changed (local branch `prep/message-request-v1`, not pushed, no PR): new `contracts/schemas/message-request.v1.json`, its valid and invalid fixtures, the INVENTORY row, the migration note in `contracts/README.md`, a root in the `customer` list of `tools/codegen/ts.json`, the regenerated Go, Pydantic and TypeScript models and registries and `shape_refusals.json` (all by `generate.py --write` and `shape_refusals.py --write`), `tools/contracts/test_message_request.py`, `services/core/edge/session/messagerequest_test.go`, and the expected customer parser lists in `tools/codegen/test_codegen.py` and `ui/test/contracts.test.mjs`.
- Why: PERD 39.2 lists the two message bodies but no contract held them; the Frontend Engineer's noema#656 asks for one schema-defined representation of "None of these", and ruling A275 says it is the core-minted `none_option_id` carried as `option_id`. No route and no input gateway change (EL's lane).
- Risks, and how they are contained: PERD 39.2 does not say how `text`, `option_id` and `clarification_id` combine. The schema takes the safer (stricter) reading, flagged in its description, `contracts/README.md` and here for the Architect: a loosening is additive, a tightening is breaking. `none_option_id` is not yet a member of the clarification object, so nothing can issue the value yet.
- Tests added (REQ-API-001, REQ-INP-001, REQ-INP-003, REQ-AMB-005, REQ-UI-002, INV-12): `test_message_request.py`; the body-guard test; the fixtures through `validate.py`, the Go round trip, the Pydantic tests and the UI parser tests. Red first at `91df4891`.
- Regression cases added: none (new contract).
- Debt entries: none filed. Known gap noted in `contracts/README.md` (Pydantic reads an optional `null` as absent).
- Conflicts raised: none. Open question for the Architect: the three unstated combination clauses.

## Pre-work for the next agent
- Exact next step: EL wires the two routes (the first message requires `text`); C1 adds `none_option_id` to the clarification object and to `overlay-card.v1` after noema#615 (A275).
- Open questions (Architect): text or option, not both; free text may answer; one schema for two routes or two.
- Files to read first: `contracts/schemas/message-request.v1.json`, `tools/contracts/test_message_request.py`.
- Do not touch: `tools/contracts/digit_run.py`, `clarification.v1.json` and `overlay-card.v1.json` (other agents).
