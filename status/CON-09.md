# Status · CON-09 · message-request.v1 (the body of POST /v1/conversations/{conversation_ref}/messages)

Date: 2026-11-19 · Owner: helper session for EL; unreviewed input · Status: red pushed, green committed locally, not pushed · REQ-API-001, REQ-INP-004; INV-12, AGENT-15 · Interim ruling A336, relayed, not yet committed. A task row CON-09 may need adding to `tasks.csv` (the Repo Steward writes it; not done here).

# Session 2026-11-19 · CON-09

## Session summary
- What changed (PR links): new `contracts/schemas/message-request.v1.json` (PERD v1.23 section 39.2: `request_id`, `text?`, `option_id?`, `clarification_id?`; closed), INVENTORY row, README migration note, 6 valid and 21 invalid fixtures, `tools/contracts/test_message_request.py`, regenerated Go and Pydantic models, registries and `shape_refusals.json` (by the generators). PR: see the PR for branch `feat/CON-09-message-request`.
- Why: the route for follow-up messages needs a contract to validate against; PERD 39.2 gives the fields, and `/contracts` must define every field (AGENTS.md section 7, APIs).
- Risks, and how they are contained: the shape is only a shape; core must still check that the clarification is open, the option belongs to it and the principal owns the conversation. The schema is closed, so no identity member can ride in the body (INV-12).
- Tests added (REQ-API-001, REQ-INP-004, INV-12): fixtures validated by `tools/contracts/validate.py`; `test_message_request.py` (member set, bounds equal canonical-input.v2 and clarification.v1, shared ID refs, code-point length, joined rules, trailing newline, explicit null); Go and Pydantic round trips by the existing suites.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Migration note (AGENT-15)
Additive: a new file; no other contract is edited or version-bumped; no consumer migrates. The route is a separate PR.

## Pre-work for the next agent
- Exact next step: the route validates its body against `message-request.v1` (Go type `MessageRequestV1`; JSON Schema for the joined rules, since the generated type holds shape only), then checks the clarification, option and principal.
- Open questions (owner): whether `text` beside `option_id` should be refused (Founders; PERD silent); whether the UI needs it as a ts.json root (UI owner).
- Files to read first: `contracts/schemas/message-request.v1.json`, `contracts/README.md` (section message-request.v1).
- Do not touch: `canonical-input.v2.json`, `clarification.v1.json`, `common.v1.json`.
