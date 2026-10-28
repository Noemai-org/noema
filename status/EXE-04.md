# Status · EXE-04 (Session A) · conditional writes; REJECTED_STALE_VERSION voids the grant

Updated 2026-10-28. Plan task EXE-04 (owner C1+AG; REQ-EXE-004; Needs EXE-02 and ENV-05; done when "US-19 passes; oracle shows no write applied").

## State

- Built on branch `sa/exe-04-stale-version-void`, from `main` at `0ddbfe1` (includes #87 and #89). Pull request: noema#90.
- Already in `main` before this task: every conditional write carries the grant's `target_versions` as the expected version (`controller.go` E9, `bridge/dispatch.go`); the real mock-case refuses a stale token with 409 `VERSION_MISMATCH`; the adapter classifies it as `VERSION_CONFLICT_409_412`; `Classify` maps it to `REJECTED_STALE_VERSION`. What was missing, and is built here: the **void**. A stale-version receipt left the grant spent with no RecoveryRequest to Trust and no `GRANT_VOIDED`.
- Built here: `ledger.Complete` of a `REJECTED_STALE_VERSION` receipt also voids the grant in the same transaction, after `RECEIPT`: one `recovery_request` (reason `REJECTED_STALE_VERSION`), the `recovery.requested` outbox event and `GRANT_VOIDED{REJECTED_STALE_VERSION}`. The part of a void that is shared with the pre-write voids is one function (`addRecoveryRequest`). No contract, migration, interface or threshold changed.

## Conflict: what a void does to a spent grant's status

Correction: the first version of this file said the PERD "does not say the status column must read `VOID`". That was wrong; the lines below say the grant is void and give `VOID` as a status. No line supports `SPENT` plus `GRANT_VOIDED` for one grant.

```
CONFLICT DETECTED
Affected IDs: REQ-EXE-004, REQ-EXE-001, INV-27, INV-17, US-19, EC-E11; PERD §38.2 (decision_grant); not listed in the conflicts register
Statements in conflict:
  A (the grant is void): "A version mismatch gives `REJECTED_STALE_VERSION`: the grant is void, the case returns to Trust, and there is no automatic retry." (PERD v1.17 REQ-EXE-004). "`REJECTED_STALE_VERSION` voids the grant and returns the case to Trust." (SoT §43.3; contracts/state-registry.md). "the conditional write is rejected (`REJECTED_STALE_VERSION`), no write is applied, the grant is void" (SoT US-19). "Grant | ISSUED, SPENT, VOID, EXPIRED" (PERD §28.1).
  B (a spent grant cannot become void): "`decision_grant` ... status ... ISSUED → SPENT, VOID or EXPIRED | Status only (via spend and void)" (PERD v1.17 §38.2). One transition out of `ISSUED` to one of three end states; no `SPENT → VOID`. Migration 0001 enforces it: `decision_grant_guard` raises "decision_grant: SPENT is final". REQ-EXE-001 Audit: "`GRANT_SPENT` or `GRANT_VOIDED{reason}`".
Evidence: a stale-version refusal happens at E9, after E8 has spent the grant, so the grant is `SPENT` before it can be refused. No PERD or SoT line says a grant may be both `SPENT` and `GRANT_VOIDED`. A and B cannot both hold literally.
Proposed alternatives:
  1. Allow `SPENT → VOID` only for a stale-version refusal: a new forward-only migration that changes `decision_grant_guard` (no edit to 0001), written in the same transaction as the receipt; plus a PERD amendment of §38.2 (new PERD version file and a decision record in `Noemai-org/docs`, AGENT-29). Blast radius: the guard (a P0 immutability trigger), `ledger.Complete`, the ledger tests, PERD §38.2, `docs/db_schema.md` when it exists.
  2. Keep the row `SPENT` and make `GRANT_VOIDED{REJECTED_STALE_VERSION}` plus the RecoveryRequest the void; amend §38.2 and REQ-EXE-001's Audit line so that a spent grant may also carry `GRANT_VOIDED`. Blast radius: the same PERD text; no migration; the simulation invariant would need a spec-backed change.
Safer reading applied meanwhile (AGENT-20): the guard is untouched and the code of #90 as pushed (alternative 2's behaviour) is not approved to merge. It is the smaller change and adds no write path, but nothing in the specification supports it yet.
Decision required from: founders
```

## Other decisions (for review)

1. **The receipt and the void are one transaction.** If the void cannot be written, no receipt is written; the attempt stays `DISPATCHING` and the sweeper writes `OUTCOME_UNKNOWN` for Outcome to verify (PERD 32.4; REQ-EXE-005).
2. **The simulation is back as on `main`.** The refinement of "a grant is never both spent and voided" was reverted on the founder's instruction of 28 Oct. The simulated ledger does not void on a stale receipt, so it no longer models the Postgres ledger on this point, and the 10,000-seed run does not cover the void. The Postgres tests and the e2e test do. A different invariant, if the design needs one, waits for the founder's decision on the conflict above and must cite the spec line that supports it.

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

# Session 2026-10-28 12:45 WAT · EXE-04

## Session summary
- What changed (PR links): noema#90: `execution/ledger/complete.go` (the void inside `Complete`; `addRecoveryRequest` shared with `Void`); tests in `execution/ledger` (`stale_version_test.go`, the nine-reason table now takes the ninth through a real dispatch), `execution/e2e` (US-19 against the real mock-case); READMEs, `docs/architecture.md`, the comment on `VoidReason`.
- Why: REQ-EXE-004 and INV-27: a refused version voids the grant, the case returns to Trust, no automatic retry.
- Risks, and how they are contained: a P0 path (Execution, Record). The void is in the transaction that already writes the receipt; a failure rolls both back and the sweeper takes over. Nothing re-dispatches (`TestThereIsOneDispatchAndNoRetryLoop` still passes; the new tests count dispatches).
- Tests added (REQ, INV, US or EC IDs): `TestAStaleVersionVoidsTheSpentGrantTogetherWithItsReceipt`, `TestAStaleVersionIsNeverRetriedAndIsVoidedOnce`, `TestIfTheVoidCannotBeWrittenNoStaleReceiptIsWrittenEither`, `TestOnlyAStaleVersionVoidsAGrantThatWasDispatched` (REQ-EXE-004, REQ-EXE-006, INV-27); `TestEveryVoidReasonReachesTheRecordAsWritten` (REQ-EXE-003, now with a real path for the ninth reason); `TestG2ARouteRacedByAnotherWriterAfterRevalidationIsRefusedAndTheGrantIsVoided` (US-19, EC-E11).
- Regression cases added: none (no defect in shipped behaviour; a missing step).
- Debt entries: none written (docs repository). Suggested: the e2e harness has no truth-API handle; Trust does not yet act on RecoveryRequests.
- Conflicts raised: one CONFLICT DETECTED block (the status of a spent grant that is voided), above. #90 is not approved to merge.

## Pre-work for the next agent
- Exact next step: EXE-05 (the three remaining capabilities) once the founders have reviewed this; Trust's handling of `recovery_request` belongs to the Trust task.
- Open questions (owner): the conflict above: alternative 1 (migration plus PERD §38.2 amendment) or alternative 2 (spec change, no migration) (founders, P0).
- Files to read first: `services/core/execution/ledger/complete.go`, `services/core/execution/ledger/stale_version_test.go`, `services/core/execution/e2e/g2_test.go`.
- Do not touch: `decision_grant_guard` (no bypass); the simulation files; the threshold, TTL and retry-budget values.
