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

