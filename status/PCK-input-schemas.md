# Status · PCK · the pack's tool input schemas (noema#362)

## Session 2026-11-10 · first slice
- What changed: `packs/schemas/fin/*.v1.json` (12 files), `packs/schemas/README.md`, `packs/tests/test_input_schemas.py`.
- Why: noema#362 (IL, ENV-05): the pack names an `input_schema` per capability and none existed; the tool facade must publish the same tools to the arms.
- Rule: caller-supplied fields only, closed schemas; no tenant, principal or derived field (IL's yes to the default, 10 Nov).
- Tests (REQ-CAP-001, REQ-CAP-002, INV-12): seen failing first; every permitted capability has its file, none for a prohibited one, each closed and free of identity or derived fields, each refuses `tenant_id`, `principal_id`, `epoch`.
- Least-sourced choice: `lookup_transaction_history` takes `account_ref`, `from`, `to` (the descriptor has no history entity; the business meaning says "within a time window"). IL or the Founders correct it in review.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the [OPEN] entry in `docs/open-decisions.md` recording the rule (a docs PR).
- Files to read first: `packs/fintech_v1.json`, `synthetic/mock_case/internal/api/openapi.json`.
