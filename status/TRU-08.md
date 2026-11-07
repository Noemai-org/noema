# TRU-08 · policy publication

State: slice 1 in review (Draft, stacked on noema#163). Owner per tasks.csv: C1; built by the Engineering Lead at the Founder's instruction of 7 Nov 2026.

# Session 2026-11-07 12:15 WAT · TRU-08

## Session summary
- What changed: `trust/policystore` and migration `0015_policy_publication.sql` (number by Founder decision D12): the policy versions a tenant has published, the active version of a policy_id (the highest), and the refusal of the same version or a downgrade by a database trigger. E4 reads it end to end in `adapters/outcome_e2e/policy_store_test.go`.
- Why: close the "active policy version" limitation properly (the stand-in binder always returned the grant's own version).
- Risks, and how they are contained: the void is lazy at E4 by design: Founder decision D8 (7 Nov 2026) rules out an eager void at publication, and only `execution_rw` writes `decision_grant.status`. DEBT-23 (docs#131) stays open for the composite production binder, the control plane, the policy document store and `AT_ADMISSION` rules.
- Tests added: REQ-TRU-005 and REQ-OPS-003 in part; INV-09, INV-21. `trust/policystore/store_test.go` (7 tests plus a catalog test) and the three end-to-end tests.
- Regression cases added: the downgrade and the unpublished-policy cases.
- Debt entries: DEBT-23.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the composite production binder; then the control plane (route names: Founder decision D14); no eager void (D8).
- Open questions (owner): migration number (Steward, noema#183).
- Files to read first: `trust/policystore/doc.go`, `migrations/0015_policy_publication.sql`.
- Do not touch: `execution/controller` E4 (noema#163).
