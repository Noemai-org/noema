# Status · EXE-04 (Session A) · conditional writes; REJECTED_STALE_VERSION voids the grant

Updated 2026-10-28. Plan task EXE-04 (owner C1+AG; REQ-EXE-004; Needs EXE-02 and ENV-05; done when "US-19 passes; oracle shows no write applied").

## State

- Built on branch `sa/exe-04-stale-version-void`, from `main` at `0ddbfe1` (includes #87 and #89). Pull request: see the message log below.
- Already in `main` before this task: every conditional write carries the grant's `target_versions` as the expected version (`controller.go` E9, `bridge/dispatch.go`); the real mock-case refuses a stale token with 409 `VERSION_MISMATCH`; the adapter classifies it as `VERSION_CONFLICT_409_412`; `Classify` maps it to `REJECTED_STALE_VERSION`. What was missing, and is built here: the **void**. A stale-version receipt left the grant spent with no RecoveryRequest to Trust and no `GRANT_VOIDED`.
- Built here: `ledger.Complete` of a `REJECTED_STALE_VERSION` receipt also voids the grant in the same transaction, after `RECEIPT`: one `recovery_request` (reason `REJECTED_STALE_VERSION`), the `recovery.requested` outbox event and `GRANT_VOIDED{REJECTED_STALE_VERSION}`. The part of a void that is shared with the pre-write voids is one function (`addRecoveryRequest`). No contract, migration, interface or threshold changed.

## Agent decisions (for review)

1. **The grant row stays `SPENT`.** `decision_grant_guard` (migration 0001) makes any status other than `ISSUED` final, so a spent grant cannot become `VOID`. The PERD says "the grant is void" and the Record says so (`GRANT_VOIDED`), but it does not say the status column must read `VOID`. The safer reading: no migration, no change to the guard. The void is the Record event and the RecoveryRequest.
2. **The receipt and the void are one transaction.** If the void cannot be written, no receipt is written; the attempt stays `DISPATCHING` and the sweeper writes `OUTCOME_UNKNOWN` for Outcome to verify. This never reads a write as success or as stale without the Record saying so (PERD 32.4; REQ-EXE-005).
3. **The simulation invariant "a grant is never both spent and voided" is refined, not dropped.** A spent grant may be voided only with reason `REJECTED_STALE_VERSION`, only when it has exactly one such receipt, and every such receipt must have exactly one void and one RecoveryRequest. The 10,000-seed run fails (5 seeds listed) when the void is removed from the simulated ledger, so the rule bites.

## Open

- **ENV-05 is a Needs task.** Its board status is not visible to this session. The part EXE-04 uses (the case adapter and mock-case) is in `main` under ENV-08 and ENV-03. Started on the founder's instruction of 28 Oct.
- **"Oracle shows no write applied".** The e2e harness holds no handle on mock-case's truth API by design (`execution/e2e/README.md`), so the test reads the case through the runtime client (its queue and version token are the other writer's). mock-case's own contract test (`TestRouteWithExpectedVersion`) already shows that a refused route applies nothing. If the founder wants the e2e to read the truth API too, that is a harness change.
- **Not built, and not this task:** Trust acting on the `recovery_request` ("re-evaluate against the new state", PERD 30.2); the `EscalateCase` workflow still ends a stale route as `ROUTE_NOT_ACKNOWLEDGED` and does not read the RecoveryRequest.
- **Observation, not changed:** executing a spent grant again runs E7 before the spend check, so it can append `REVALIDATION_FAILED` for a grant that is already spent. It makes no write and no second void.
- **Tushe:** version tokens on Tushe's case routes are still an [ASM] (SOT-C-08). Everything here is proved against mock-case only. Tushe-dependent work stays paused.

## Messages

### 2026-10-28 (time not recorded) accepted · Founder → Session A
- Purpose: "4. Then start EXE-04, on the merged #87 base. Report the head SHA and run links when it is ready for review. Do not merge. Log every message in status/ENV-03.md." (the full message is in `status/ENV-03.md`).
- Used it for: this task and pull request.
- Evidence: this pull request.

# Session 2026-10-28 12:45 WAT · EXE-04

## Session summary
- What changed (PR links): `execution/ledger/complete.go` (the void inside `Complete`; `addRecoveryRequest` shared with `Void`); tests in `execution/ledger` (`stale_version_test.go`, the nine-reason table now takes the ninth through a real dispatch), `execution/controller` (simulation world and invariants) and `execution/e2e` (US-19 against the real mock-case); READMEs, `docs/architecture.md`, the comment on `VoidReason`.
- Why: REQ-EXE-004 and INV-27: a refused version voids the grant, the case returns to Trust, no automatic retry.
- Risks, and how they are contained: a P0 path (Execution, Record). The void is in the transaction that already writes the receipt; a failure rolls both back and the sweeper takes over. Nothing re-dispatches (`TestThereIsOneDispatchAndNoRetryLoop` still passes; the new tests count dispatches).
- Tests added (REQ, INV, US or EC IDs): `TestAStaleVersionVoidsTheSpentGrantTogetherWithItsReceipt`, `TestAStaleVersionIsNeverRetriedAndIsVoidedOnce`, `TestIfTheVoidCannotBeWrittenNoStaleReceiptIsWrittenEither`, `TestOnlyAStaleVersionVoidsAGrantThatWasDispatched` (REQ-EXE-004, REQ-EXE-006, INV-27); `TestEveryVoidReasonReachesTheRecordAsWritten` (REQ-EXE-003, now with a real path for the ninth reason); `TestG2ARouteRacedByAnotherWriterAfterRevalidationIsRefusedAndTheGrantIsVoided` (US-19, EC-E11); the simulation invariants (REQ-EXE-004).
- Regression cases added: none (no defect in shipped behaviour; a missing step).
- Debt entries: none written (docs repository). Suggested: the e2e harness has no truth-API handle; Trust does not yet act on RecoveryRequests.
- Conflicts raised: none. Agent decision 1 above is the nearest thing to one.

## Pre-work for the next agent
- Exact next step: EXE-05 (the three remaining capabilities) once the founders have reviewed this; Trust's handling of `recovery_request` belongs to the Trust task.
- Open questions (owner): whether the e2e should read mock-case's truth API (founder); whether a spent grant's row should read `VOID` (it would need a migration that relaxes `decision_grant_guard`; founders, P0).
- Files to read first: `services/core/execution/ledger/complete.go`, `services/core/execution/ledger/stale_version_test.go`, `services/core/execution/e2e/g2_test.go`.
- Do not touch: `decision_grant_guard`; the threshold, TTL and retry-budget values.
