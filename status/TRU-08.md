# TRU-08 · policy publication

State: slice 1 in review (Draft, stacked on noema#163). Owner per tasks.csv: C1; built by the Engineering Lead at the Founder's instruction of 7 Nov 2026.

# Session 2026-11-07 12:15 WAT · TRU-08

## Session summary
- What changed: `trust/policystore` and migration `0012_policy_publication.sql` (provisional number, ruling A34): the policy versions a tenant has published, the active version of a policy_id (the highest), and the refusal of the same version or a downgrade by a database trigger. E4 reads it end to end in `adapters/outcome_e2e/policy_store_test.go`.
- Why: close the "active policy version" limitation properly (the stand-in binder always returned the grant's own version).
- Risks, and how they are contained: see DEBT-23 (docs#131): the void is lazy at E4, not eager at publication; the composite production binder is composed by hand in the test.
- Tests added: REQ-TRU-005 and REQ-OPS-003 in part; INV-09, INV-21. `trust/policystore/store_test.go` (7 tests plus a catalog test) and the three end-to-end tests.
- Regression cases added: the downgrade and the unpublished-policy cases.
- Debt entries: DEBT-23.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the eager void on publication, after a ruling on which role changes `decision_grant.status` (ruling A6 gives it to `execution_rw`); then the composite binder; then the control plane.
- Open questions (owner): migration number (Steward, noema#183); the void role (Architect).
- Files to read first: `trust/policystore/doc.go`, `migrations/0012_policy_publication.sql`.
- Do not touch: `execution/controller` E4 (noema#163).
