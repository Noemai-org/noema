# Status · TRU-07 follow-up: the single-use spend of a given confirmation (noema#603)

Updated 2026-11-15. Owner: C1. Ledger: migration 0028 on noema#183.

# Session 2026-11-15 · TRU-07 confirmation spend

## Session summary
- What changed (PR links): noema#603. `migrations/0028_confirmation_spend.sql` (`confirmation_spend`, insert-only, `UNIQUE (tenant_id, confirmation_event_id)` and `UNIQUE (tenant_id, decision_id)`, a trigger that only an ALLOW decision of the same tenant may spend, tenant guard and row-level security as for `decision_record`); `trust/recorder`: `Request.ConfirmationEventID`, the insert after the decision row, `ErrConfirmationSpent`, every grant follows the spent confirmation; one reviewed-path line in `platform/db/rls_paths_test.go`.
- Why: noema#572 blocking finding 1 (the Security Reviewer): a given confirmation was not single use. EL's answer to Q-C put the check and the spend in `recorder.Writer`'s transaction and named C1 as the author of the migration and the `Writer` change.
- Risks, and how they are contained: (a) a foreign key from `confirmation_spend` to `decision_record` would turn the table's insert-only refusal of TRUNCATE into another error code (0A000), which `TestTheDecisionRecordIsInsertOnlyAndOnlyTrustWritesIt` pins; the spend names its ALLOW decision by a trigger instead, as `decision_grant` does. (b) A retry of the same input that names another confirmation than the stored ALLOW followed is `ErrDecisionConflict` (A233, interim), nothing spent or written; a stored refusal that named a confirmation is returned for its own retry as before. (c) `deploy/compose/postgres/test_init.sh` cannot run as root locally; the `compose-scripts` check runs it.
- Tests added (REQ-TRU-008, REQ-TRU-009, INV-03, INV-17, INV-21, INV-25): `trust/recorder/confirmation_spend_test.go` (seven tests), seen failing first (local run against the stub; then the CI red run).
- Regression cases added: the concurrent one (two evaluations of two inputs, one confirmation: one ALLOW commits, the other is `ErrConfirmationSpent`), real PostgreSQL.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the intake PR (noema#572) adds the `Confirmations` adapter's event ID to `Recorder.Record` and maps `ErrConfirmationSpent` to UNKNOWN; EL's Recorder adapter in `cmd/core` passes the event ID through and nothing else.
- Open questions (owner): none for this PR. A230 and A233 are interim until docs#185 merges.
- Files to read first: `services/core/migrations/0028_confirmation_spend.sql`, `services/core/trust/recorder/recorder.go` (Write), `confirmation_spend_test.go`.
- Do not touch: `decision_record` and `decision_grant` (0012), `inputkey.go` (A186).

# Session 2026-11-16 · TRU-07 A230 and A233 (noema#603)
- What changed: A233 (a retry naming another confirmation than the stored ALLOW is `ErrDecisionConflict`) and A230 (the writer refuses an ALLOW whose confirmation was given for another plan than the steps carry: `Request.ConfirmationPlanHash`, `ErrConfirmationPlanMismatch`, checked before anything is written; the spend row records `plan_hash`). Migration 0028 gains `plan_hash` (the migration is not merged, so it is edited, not superseded).
- Tests added (REQ-TRU-008, REQ-TRU-009, INV-17, INV-21): `TestARetryNamingAnotherConfirmationThanTheStoredDecisionIsATypedConflict`, `TestAnAllowWhoseConfirmationWasGivenForAnotherPlanIsRefused`; seen failing first (local, then CI red at `7952add5`).
- Not done: the intake side (noema#572) reads the confirmation's plan hash from the store and passes it in `RecordRequest`; the confirmation store and its adapter are EL's.

# Session 2026-11-16 (later) · TRU-07 the confirmation is checked in the database (noema#603; the Security Reviewer's blocking 1 on noema#572)
- What changed: migration 0028 gains `confirmation_resolution` (an accepted confirmation that resolved to a fresh evaluation: event ID, request ID, binding hash, expiry; insert-only, tenant-scoped, a request answered once) and the spend now needs the confirmation: a trigger refuses the spend unless the confirmation exists for the tenant, has not expired by the database's clock (`statement_timestamp()`, never the caller's) and has the binding hash the writer computed from the ALLOW's own step (`confirm.BindingHash` of the eight values of SoT 43.2a). One answer for the three (`ErrConfirmationNotValid`). `recorder.RecordConfirmation` is the supported path for the confirmation store (EL's adapter) to record one. The writer needs exactly one step after a confirmation. The Record shows the spend: `confirmation_event_id` and `confirmation_binding_hash` on DECISION_MADE, `confirmation_event_id` on GRANT_ISSUED.
- A decision of mine for the Architect: **no new event type.** PERD 32.2 (normative) lists no spend event, and a new type is a PERD version plus a contract change for both Founders; the spend rides on the two events of the same transaction instead. If a dedicated `CONFIRMATION_SPENT` is wanted, it is a follow-up after the PERD change.
- Risks: the expiry uses the database's clock, so the tests use real times (through `platform/clock`); a confirmation recorded with a binding from another policy-version form than the grant's bound policy (`fin.pol@3.1`) is refused (EL's adapter must compute the binding with the bound policy); the migration is edited in place because it is not merged.
- Tests added (REQ-TRU-008, REQ-TRU-009, INV-17, INV-25): a confirmation that does not exist, another tenant's, an expired one and eight bindings each changed in turn; the Record's evidence; the recording's form, once-only and insert-only rows, and tenant layers; seen failing first (local, then CI red at `9e5e0cb4`). Eight mutants of the migration: no expiry check, no binding check, no requires-confirmation trigger, no tenant guard, no row security, no request key and no immutability on the resolution are each caught; "no existence check" is an equivalent mutant (a missing row reads as a different binding, so the binding check refuses it too), kept as defence in depth.
- Not done: the confirmation store itself and its adapter (EL); the intake maps `ErrConfirmationNotValid` like every Recorder error (UNKNOWN, never ALLOW), so the outward answer is the same for all of them.


# Session 2026-11-16 (round 3) · TRU-07 the Security Reviewer's re-read of noema#603 at 9452a364
- What changed: `TestASpendCanNameOnlyARecordedAllowDecision`: the second statement passed three arguments to a statement with four placeholders, so pgx refused it on the client before any trigger ran and `err == nil` held whatever the database did (the finding). It now passes the binding hash and asserts the SQLSTATE and the requires-allow message through `pgRefusal`. `TestTheTenantBindingIsEnforcedByTheGuardAndByRowSecurityEachOnItsOwn`: a `t.Cleanup` switches the three triggers back on, so a failure part-way cannot leave one off (note 6). `trust/recorder/doc.go` says what the database does not enforce (note 1).
- Tests (REQ-TRU-008, INV-17): the case passes on the unchanged migration by design (it is a test that could not fail, not a defect); checked with a mutant: with the `confirmation_spend_requires_allow` trigger removed, both the REJECT case and the no-such-decision case fail (they get the confirmation trigger's message instead).
- Notes answered on the PR: 2 and 3 are the adapter's (EL), 4 is the Architect's question already asked, 5 is accepted by the reviewer.
- Debt entries: none. Conflicts raised: none.
