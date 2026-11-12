# Status · CON-03 follow-up (Core Engineer 1) · intent-object.v2

Updated 2026-11-12. Ruling A163 (the Architect's message of 12 Nov 2026, relayed, to be committed): widening the `asserted_by` pattern is a contract bump, in its own small pull request, with C2 (AGENT-15). Context: noema#467 (A149 session fields) and noema#475.

## Session 2026-11-12 20:00 WAT · intent-object.v2
### Session summary
- What changed: `contracts/schemas/intent-object.v2.json` (v1 with `schema_version` major 2 and `principal.asserted_by` matching `^[a-z][a-z0-9_-]*$`), its fixtures (the v1 fixtures at 2.0, one valid principal as the synthetic IdP mints it, four invalid), `tools/contracts/test_intent_object_v2.py`, the inventory row, the `contracts/README.md` section with the migration note, and the regenerated Go and Pydantic models (`make codegen`). v1 is not edited (its SHA-256 is pinned by a test).
- Why: the session's verified issuer is `noema-poc-idp` (ruling A1); v1's pattern refused it, which was the CONFLICT block of noema#467.
- Risks, and how they are contained: a reader that still validates against v1 refuses a hyphenated issuer; none is in production (no producer writes an Intent Object outside tests and fixtures), and the migration note says writer, validators and readers move to v2 together. The pattern is anchored over the whole string (A138).
- Tests (INV-12, AGENT-15, A163): six in `test_intent_object_v2.py`: v1 bytes pinned; only the version and `asserted_by` differ; a v1 document is not a valid v2 document; the hyphenated issuer is valid in v2 and refused by v1; the pattern refuses trailing newline, leading hyphen, upper case, space, empty, leading digit and a dot; every v1 fixture has a v2 twin. Four mutants of the schema (pattern as v1, any major, no end anchor, no leading letter) each fail a named test.
- Regression cases added: the invalid fixtures. Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the orchestrator's Intent Object composition (EL, STM-01) validates against v2 and copies `principal_type`, `authn_ref` and `asserted_by` from the session (A149); noema#467 supplies them. Consumers of v1 (`proposer_swap` tests and fixtures) stay on v1 until they move.
- Files to read first: `contracts/README.md` (Intent Object v2), `contracts/schemas/intent-object.v2.json`.
- Do not touch: `contracts/schemas/intent-object.v1.json`.
