
# Session 2026-11-08 · TRU-05 slice 2 (noema#168)

## Session summary
- What changed: the Architect's ruling of 8 Nov 2026 on noema#329, all four rules in the database. (d) a CHECK `decision_record_bound_fields`; (a) to (c) in `decision_grant_requires_allow`. Fixtures that insert records and grants directly now carry the bound fields (`testpg.BoundFieldsSQL`, `testpg.GrantBoundSQL`, `testpg.DecisionDocJSON`).
- Tests added (REQ-TRU-008, INV-17): four, in `platform/db/grant_matches_record_test.go`, seen failing before the migration change.
- Risks: a grant fixture that omits the intent hash, policy version or a valid expiry is now refused; every existing fixture was updated and the whole `services/core` suite passes locally.
- Not done: SR's note 5 (owner policy under a non-superuser owner stays unproven). #168 needs a fresh reviewer READY at the new head.

## Pre-work for the next agent
- Branches stacked on #168 (#197, #237, #261, #233) insert records or grants directly in tests: merge this head in and add the bound fields.
