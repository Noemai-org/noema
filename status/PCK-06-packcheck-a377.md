# PCK-06 packcheck A377: state

State: red pushed (PR #787, draft); green local, not pushed. Blocked part: everything needing `params`, `case_reasons`, `write_when` (open #765).

# Session 2026-11-19 WAT · PCK-06

## Session summary
- What changed (PR links): noema#787. Packcheck check `roles` (ROLE:<role> outside A1 refused); edge-condition tests; depends_on README note.
- Why: A377 (interim, relayed, not yet committed) rulings 10 and the ROLE line.
- Risks, and how they are contained: refuses more only; real packs still pass.
- Tests added (REQ, INV, US or EC IDs): ApproverRoles, EdgeConditions, bad pack role_01 (REQ-CAP-001).
- Regression cases added: role_01_approver_role_outside_a1.json.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after #765 merges, add the param-coverage check (items 4/9, g3 `case_id = {fact: case_id}`), slot param `kind` (item 13, additive optional member in action-graph.v2, AGENT-15 README note) and `case_reasons` format/membership. Start from main with #765 in it; do not base on its branch.
- Open questions (Founder): slot param `kind` member shape (e.g. `{slot, kind}`) is not in the rulings.
- Files to read first: tools/packcheck/checks.py, contracts/schemas/action-graph.v2.json.
- Do not touch: #765 files until it merges.
