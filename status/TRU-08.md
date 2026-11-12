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

# Session 2026-11-07 (later) · TRU-08

## Session summary
- What changed: SR's notes on noema#259. Notes 1 and 2: a version has one spelling (CHECK and Go pattern: no leading zeros, no third component of 0, components of at most nine digits). Note 4: the insert-only tests assert the SQLSTATE and the trigger is exercised as the owner (23000); Trust's attempts are asserted as 42501.
- Why: SR review at `5b12ae1`; a typo such as `2147483647.0` could never be superseded (no revoke), and `3.1` versus `3.1.0` made E4's exact-string compare disagree with the numeric key.
- Risks: a version such as `1.0.0` is now refused (use `1.0`). Packs use two components today (`3.1`, `3.2`). Reversible by a forward migration if a Founder prefers to admit the trailing zero.
- Tests added (REQ-TRU-005, INV-09): `TestAMalformedPolicyIDOrVersionIsRefused` (5 new cases), `TestTheDatabaseRefusesANonCanonicalOrOversizedVersionAndTheTriggerRefusesEdits`; `TestVersionsCompareAsNumbersAndNotAsText` and the downgrade test now expect ErrRequest for non-canonical spellings. Seen failing first.
- Not done: note 3 (where the control-plane credential injects `PublishedBy`: waits for OD-P-79); note 5 (owner policy under a non-superuser owner is catalog-only; unproven in CI).

# Session 2026-11-11 · TRU-08 follow-up (the production Binder, DEBT-23)

## Session summary
- What changed: `execution/ledger/binder.go`: `StoreBinder`, the production `controller.Binder`. Its database half (the active policy version and "is the decision still an ALLOW under it") reads `policy_publication` and `decision_record` as the tenant's Execution login; the intent hash (E3) and the plan (E5) are ports (`IntentSource`, `PlanSource`) that other tasks build and `cmd/core/compose.go` plugs in; the kill switch reads through the Ledger.
- Why: the TRU-08 slice 1 note named it as the next step; the G1 slice needs E4 to read the real store, not the stand-in binder.
- Risks, and how they are contained: it duplicates the one active-version query of `trust/policystore` because Execution never imports Trust's internals (the e2e test of slice 1 proves the two agree). The control plane (publish route) and the policy document store stay open under DEBT-23.
- Tests added (REQ-TRU-005, REQ-EXE-001, INV-09, INV-12): `TestTheActivePolicyVersionIsTheHighestPublishedOne`, `TestADecisionIsStillAllowedOnlyWhileItsPolicyVersionIsActive`, `TestAnUnknownDecisionOrAnUnpublishedPolicyIsNeverAllowed`, `TestARecordedDecisionThatIsNotAnAllowIsNeverAllowed`, `TestAnotherTenantsGrantIsNeverAllowedThroughThisTenantsLogin`, `TestTheBinderNeedsADeadlineAndItsSources`, `TestTheBinderPassesTheIntentAndPlanReadsThroughToTheirSources`. Eight mutants (outcome ignored, active version ignored, an empty active version accepted, a missing decision allowed, versions ordered as text, an unwired source answering empty, and two more) are killed by named tests; one is equivalent: dropping the tenant filter on the decision read changes nothing while row-level security and the second tenant-bound read hold (defence in depth, kept).
- Regression cases added: the same tests.
- Debt entries: DEBT-23 stays open (control plane and document store).
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: `cmd/core/compose.go` plugs `StoreBinder` into the controller once the intent and plan sources exist; then the control-plane publish route (route names: D14) with the EDG-01 role check.
- Open questions (owner): who builds the intent and plan sources (the Steward names the task).
- Files to read first: `execution/ledger/binder.go`, `trust/policystore/doc.go`.
- Do not touch: `execution/controller` E4.
