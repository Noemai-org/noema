# Status · EXE-04 (Session A) · conditional writes; REJECTED_STALE_VERSION voids the grant

Updated 2026-10-28. Plan task EXE-04 (owner C1+AG; REQ-EXE-004; Needs EXE-02 and ENV-05; done when "US-19 passes; oracle shows no write applied").

## State

- Built on branch `sa/exe-04-stale-version-void`, from `main` at `0ddbfe1` (includes #87 and #89). Pull request: noema#90.
- Already in `main` before this task: every conditional write carries the grant's `target_versions` as the expected version (`controller.go` E9, `bridge/dispatch.go`); the real mock-case refuses a stale token with 409 `VERSION_MISMATCH`; the adapter classifies it as `VERSION_CONFLICT_409_412`; `Classify` maps it to `REJECTED_STALE_VERSION`. What was missing, and is built here: the **void**. A stale-version receipt left the grant spent with no RecoveryRequest to Trust and no `GRANT_VOIDED`.
- Built here: `ledger.Complete` of a `REJECTED_STALE_VERSION` receipt also voids the grant in the same transaction, after `RECEIPT`: one `recovery_request` (reason `REJECTED_STALE_VERSION`), the `recovery.requested` outbox event and `GRANT_VOIDED{REJECTED_STALE_VERSION}`. The part of a void that is shared with the pre-write voids is one function (`addRecoveryRequest`). No contract, migration, interface or threshold changed.

## Conflict: what a void does to a spent grant's status (resolved by the founders, 28 Oct)

The conflict block raised on 28 Oct (PERD v1.17 REQ-EXE-004, SoT §43.3 and US-19 say the grant is void; §38.2 allowed only `ISSUED → SPENT, VOID or EXPIRED`; `decision_grant_guard` refused `SPENT → VOID`) is settled: the founders approved Option 1 in principle, with conditions.

- Docs: [docs#66](https://github.com/Noemai-org/docs/pull/66): PERD v1.18 §38.2 permits `SPENT → VOID` only for a confirmed `REJECTED_STALE_VERSION` refusal, and `DEC-2026-10-28-spent-grant-void`. Not merged.
- Migration: noema#94 (`0002_grant_spent_void.sql`; the same files are in this branch). Not merged.
- This PR: `ledger.Complete` makes the move in the receipt's transaction; the tests assert `VOID`; the Record keeps `GRANT_SPENT` and `GRANT_VOIDED` names `from_status` `SPENT`, `to_status` `VOID` and the receipt.
- **The exact confirmation check** (in the trigger): the grant has exactly one receipt; it is the receipt of the attempt `grant_spend` recorded, for the same tenant; its state is `REJECTED_STALE_VERSION`; the part of its `raw_status` before the first colon is `VERSION_CONFLICT_409_412`; it has no external acknowledgement. It is not a read by Outcome (the verifier is not built and its read is asynchronous); the founders should confirm that reading (docs#66, decision record §3 item 1).
- Not merged until the migration, the amendment, the decision record and the regression evidence are reviewed.

## Other decisions (for review)

1. **The receipt and the void are one transaction.** If the void cannot be written, no receipt is written; the attempt stays `DISPATCHING` and the sweeper writes `OUTCOME_UNKNOWN` for Outcome to verify (PERD 32.4; REQ-EXE-005).
2. **The simulation is as on `main`, by the founder's instruction of 28 Oct.** The simulated ledger does not void on a stale receipt, so the original invariant "a grant is never both spent and voided" still holds there and the 10,000-seed run does not cover the void. With Option 1 the Postgres ledger voids a spent grant. The spec lines that would support a different invariant are PERD v1.18 REQ-EXE-001 Audit ("for a confirmed `REJECTED_STALE_VERSION` refusal both, `GRANT_SPENT` first") and REQ-EXE-004 (docs#66, not merged). I have not changed the invariant; the proposal is in the founder report and waits for approval.

## Open

- **ENV-05 is a Needs task.** Its board status is not visible to this session. The part EXE-04 uses (the case adapter and mock-case) is in `main` under ENV-08 and ENV-03. Started on the founder's instruction of 28 Oct. ENV-05 is paused unless the founder confirms it is needed now (28 Oct).
- **"Oracle shows no write applied".** Decided by the founder on 28 Oct: keep the read through the runtime client; no truth-API handle is added. The e2e harness holds none (`execution/e2e/README.md`), so the test reads the case through the runtime client (its queue and version token are the other writer's). mock-case's own contract test (`TestRouteWithExpectedVersion`) already shows that a refused route applies nothing.
- **Not built, and not this task:** Trust acting on the `recovery_request` ("re-evaluate against the new state", PERD 30.2); the `EscalateCase` workflow still ends a stale route as `ROUTE_NOT_ACKNOWLEDGED` and does not read the RecoveryRequest.
- **Observation, not changed:** executing a spent grant again runs E7 before the spend check, so it can append `REVALIDATION_FAILED` for a grant that is already spent. It makes no write and no second void.
- **Tushe:** version tokens on Tushe's case routes are still an [ASM] (SOT-C-08). Everything here is proved against mock-case only. Tushe-dependent work stays paused.

## Messages

### 2026-10-28 (time not recorded) accepted · Founder → Session A
- Purpose: "4. Then start EXE-04, on the merged #87 base. Report the head SHA and run links when it is ready for review. Do not merge. Log every message in status/ENV-03.md." (the full message is in `status/ENV-03.md`).
- Used it for: this task and pull request.
- Evidence: noema#90.

### 2026-10-28 (time not recorded) accepted · Founder → Session A
- Purpose: "#90 is not approved to merge yet. Do not merge. Pause ENV-05 ... Revert the refinement. Restore the original invariant. ... Keep the read through the runtime client. Do not add a truth API handle." (the full message is in `status/ENV-03.md`).
- Used it for: the conflict block above, the reverted simulation files, the oracle note.
- Evidence: noema#90.

### 2026-10-28 (time not recorded) accepted · Founder → Session A
- Purpose: "the founder approves Option 1 in principle for #90, with these conditions. Do not merge #90 until the migration, the specification amendment, the decision record and the regression evidence are reviewed. 1. Docs first ... 2. Noema migration ... 3. Tests ... 4. After the docs amendment merges, re-point the vendored copies ... 5. Report ... Do not merge." (the full message is in `status/ENV-03.md`, added by noema#94).
- Used it for: docs#66, noema#94, and this branch's update.
- Evidence: docs#66, noema#94, noema#90.

# Session 2026-10-28 12:45 WAT · EXE-04

## Session summary
- What changed (PR links): noema#90: `execution/ledger/complete.go` (the void inside `Complete`; `addRecoveryRequest` shared with `Void`); tests in `execution/ledger` (`stale_version_test.go`, the nine-reason table now takes the ninth through a real dispatch), `execution/e2e` (US-19 against the real mock-case); READMEs, `docs/architecture.md`, the comment on `VoidReason`.
- Why: REQ-EXE-004 and INV-27: a refused version voids the grant, the case returns to Trust, no automatic retry.
- Risks, and how they are contained: a P0 path (Execution, Record). The void is in the transaction that already writes the receipt; a failure rolls both back and the sweeper takes over. Nothing re-dispatches (`TestThereIsOneDispatchAndNoRetryLoop` still passes; the new tests count dispatches).
- Tests added (REQ, INV, US or EC IDs): `TestAStaleVersionVoidsTheSpentGrantTogetherWithItsReceipt`, `TestAStaleVersionIsNeverRetriedAndIsVoidedOnce`, `TestIfTheVoidCannotBeWrittenNoStaleReceiptIsWrittenEither`, `TestOnlyAStaleVersionVoidsAGrantThatWasDispatched` (REQ-EXE-004, REQ-EXE-006, INV-27); `TestEveryVoidReasonReachesTheRecordAsWritten` (REQ-EXE-003, now with a real path for the ninth reason); `TestG2ARouteRacedByAnotherWriterAfterRevalidationIsRefusedAndTheGrantIsVoided` (US-19, EC-E11).
- Regression cases added: none (no defect in shipped behaviour; a missing step).
- Debt entries: none written (docs repository). Suggested: the e2e harness has no truth-API handle; Trust does not yet act on RecoveryRequests.
- Conflicts raised: one CONFLICT DETECTED block (the status of a spent grant that is voided), resolved by Option 1 (above). #90 is not approved to merge.

## Pre-work for the next agent
- Exact next step: EXE-05 (the three remaining capabilities) once the founders have reviewed this; Trust's handling of `recovery_request` belongs to the Trust task.
- Open questions (owner): the review of docs#66, noema#94 and this PR (founders, P0); the simulation invariant proposal; the confirmation reading (no Outcome verdict).
- Files to read first: `services/core/execution/ledger/complete.go`, `services/core/execution/ledger/stale_version_test.go`, `services/core/execution/e2e/g2_test.go`.
- Do not touch: `decision_grant_guard` (changed only by migration 0002; no bypass); the simulation files until the invariant is approved; the threshold, TTL and retry-budget values.
