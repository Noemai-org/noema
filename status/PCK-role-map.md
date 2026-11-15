# Status · role-to-capability map (noema#341; ruling A3)

Updated 2026-11-08. Owner: EL (pack owner). The Benchmark Owner's arm view needs `permitted_capabilities` per role (SR F-14).

## State
- `packs/fintech/role_capabilities.json`, derived by `tools/derive_role_capabilities.py` from `packs/fintech_v1.json`, with `packs/tests/test_role_capabilities.py` (6 tests, seen failing first). Stacked on noema#253 (the A37 role rename).
- Finding: the pack's `roles_allowed` also names `customer` and `ops` on the eight **prohibited** capabilities. The map excludes them; reading `roles_allowed` directly would have offered every arm `transfer_funds`, `reverse_transaction` and the rest. `customer` and `ops` hold the same twelve; `approver` and `audit` hold none.
- Also written down in `packs/README.md`: what `subject_resource.status == 'RESOLVED'` means (the intent slot is resolved to one transaction), answering noema#346.

## Pre-work for the next agent
- When noema#253 merges, merge main into this branch.
- BO: read `roles` of the map; do not read the pack's `roles_allowed` for the arm view.
