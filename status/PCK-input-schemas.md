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

# Session 2026-11-11 · PCK input schemas (noema#417), definition of done

## Session summary
- What changed: found two unbounded inputs in my own schemas while filling row 3b of the definition of done (A131): `record_investigation_evidence.payload` had no `maxLength`, and `lookup_transaction_history` `from` and `to` used `format: date-time`, which validators do not enforce. Now: `payload` is capped at the 1 MiB body bound the mock case enforces; `from` and `to` use the committed `common.v1` Timestamp pattern, `maxLength` 30 and a newline refusal.
- Tests added (REQ-CAP-001, INV-12): `test_every_string_field_is_bounded`, `test_a_timestamp_field_is_the_committed_pattern_not_an_unenforced_format`, `test_a_value_over_the_bound_or_empty_or_of_the_wrong_type_is_refused`. Seen failing first (5 failures), then green; a mutant without the newline guard fails.
- Risks: the 1 MiB cap on `payload` is a sanity bound taken from the mock case, not a decided limit; the `lookup_transaction_history` fields remain least-sourced (IL or the Founders correct them).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the [OPEN] entry for the rule in `docs/open-decisions.md` (docs PR).
- Open questions (Founders): a tighter `payload` cap.
- Do not touch: `packs/fintech_v1.json`.
