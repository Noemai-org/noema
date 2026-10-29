# Status · EXE-04 (Session A) · conditional writes; REJECTED_STALE_VERSION voids the grant

Updated 2026-10-29. Plan task EXE-04 (owner C1+AG; REQ-EXE-004; Needs EXE-02 and ENV-05; done when "US-19 passes; oracle shows no write applied").

## State

- Built on branch `sa/exe-04-stale-version-void`, from `main` at `0ddbfe1` (includes #87 and #89). Pull request: noema#90.
- Already in `main` before this task: every conditional write carries the grant's `target_versions` as the expected version (`controller.go` E9, `bridge/dispatch.go`); the real mock-case refuses a stale token with 409 `VERSION_MISMATCH`; the adapter classifies it as `VERSION_CONFLICT_409_412`; `Classify` maps it to `REJECTED_STALE_VERSION`. What was missing, and is built here: the **void**. A stale-version receipt left the grant spent with no RecoveryRequest to Trust and no `GRANT_VOIDED`.
- Built here: `ledger.Complete` of a `REJECTED_STALE_VERSION` receipt also voids the grant in the same transaction, after `RECEIPT`: one `recovery_request` (reason `REJECTED_STALE_VERSION`), the `recovery.requested` outbox event and `GRANT_VOIDED{REJECTED_STALE_VERSION}`. The part of a void that is shared with the pre-write voids is one function (`addRecoveryRequest`). No contract, migration, interface or threshold changed.

## Conflict: what a void does to a spent grant's status (resolved by the founders, 28 Oct)

The conflict block raised on 28 Oct (PERD v1.17 REQ-EXE-004, SoT §43.3 and US-19 say the grant is void; §38.2 allowed only `ISSUED → SPENT, VOID or EXPIRED`; `decision_grant_guard` refused `SPENT → VOID`) is settled: the founders approved Option 1 in principle, with conditions.

- Docs: [docs#66](https://github.com/Noemai-org/docs/pull/66): PERD v1.18 §38.2 permits `SPENT → VOID` only for a confirmed `REJECTED_STALE_VERSION` refusal, and `DEC-2026-10-28-spent-grant-void`. Not merged.
- Migration: noema#94 (`0002_grant_spent_void.sql`; the same files are in this branch). Not merged.
- This PR: `ledger.Complete` makes the move in the receipt's transaction; the tests assert `VOID`; the Record keeps `GRANT_SPENT` and `GRANT_VOIDED` names `from_status` `SPENT`, `to_status` `VOID` and the receipt.
- **The exact confirmation check** (in the trigger, noema#94): read from the signed receipt document (`execution-receipt.v1`) and cross-checked against the row's columns and `grant_spend`. The grant has exactly one receipt; its tenant, grant, attempt and idempotency key are the spend's, in the row and in the document; `execution_state` is `REJECTED_STALE_VERSION` in both; the typed member `transport_result` is `VERSION_CONFLICT_409_412`; `external_ack` is null in the document and null or empty in the row; the document's signature is the row's. `raw_status` is a diagnostic that must agree with `transport_result`. Mapping: the real target is mock-case (Tushe has no case API: delta v1.2, worksheet AT-14), whose contract (`openapi.json`, route response 409) is `409 VERSION_MISMATCH`; the adapter gives that, and only that, the class `VERSION_CONFLICT_409_412` (raw `409:VERSION_MISMATCH`); mock-case's `412 PRECONDITION_FAILED` is a refused create and gives `DUPLICATE_CREATE` (`REJECTED_BY_TARGET`). Limitations: the receipt contract fixes the shape of `transport_result` but no value set; nothing verifies the receipt's HMAC before it is written (see "The signature question" below). It is not a read by Outcome (the verifier is not built and its read is asynchronous); the founders should confirm that reading (docs#66, decision record §3 item 1).
- Not merged until the migration, the amendment, the decision record and the regression evidence are reviewed.

## Other decisions (for review)

1. **The receipt and the void are one transaction.** If the void cannot be written, no receipt is written; the attempt stays `DISPATCHING` and the sweeper writes `OUTCOME_UNKNOWN` for Outcome to verify (PERD 32.4; REQ-EXE-005).
2. **The simulation models the one permitted exception** (founder instruction of 28 Oct, after Option 1). The simulated ledger has the grant status machine of PERD v1.18 §38.2 (`moveLocked`): `ISSUED → SPENT, VOID or EXPIRED`, and `SPENT → VOID` only when its confirmation holds (one receipt, of the spent attempt, `REJECTED_STALE_VERSION`, class `VERSION_CONFLICT_409_412`, no acknowledgement); a stale receipt voids the grant in the same step or neither is kept. The invariants: a spent grant is voided only with reason `REJECTED_STALE_VERSION` after exactly one such receipt; every stale receipt has one void and one RecoveryRequest; no duplicate void or RecoveryRequest; status agrees with the Record. `sim_guard_test.go` drives each refusal (every other status change, every other receipt, duplicate receipt, duplicate void, duplicate RecoveryRequest); each rule fails a test when removed. Limitation: the model checks the confirmation on the receipt's typed fields as the controller builds them, not the signed document or its HMAC.

## Open

- **ENV-05 is a Needs task.** Its board status is not visible to this session. The part EXE-04 uses (the case adapter and mock-case) is in `main` under ENV-08 and ENV-03. Started on the founder's instruction of 28 Oct. ENV-05 is paused unless the founder confirms it is needed now (28 Oct).
- **"Oracle shows no write applied".** Decided by the founder on 28 Oct: keep the read through the runtime client; no truth-API handle is added. The e2e harness holds none (`execution/e2e/README.md`), so the test reads the case through the runtime client (its queue and version token are the other writer's). mock-case's own contract test (`TestRouteWithExpectedVersion`) already shows that a refused route applies nothing.
- **Not built, and not this task:** Trust acting on the `recovery_request` ("re-evaluate against the new state", PERD 30.2); the `EscalateCase` workflow still ends a stale route as `ROUTE_NOT_ACKNOWLEDGED` and does not read the RecoveryRequest.
- **Observation, not changed:** executing a spent grant again runs E7 before the spend check, so it can append `REVALIDATION_FAILED` for a grant that is already spent. It makes no write and no second void.
- **Tushe:** version tokens on Tushe's case routes are still an [ASM] (SOT-C-08). Everything here is proved against mock-case only. Tushe-dependent work stays paused.

## The signature question, the provenance of "nothing applied" and the reconciliation (28 Oct, after docs#66 merged)

**Decided by the founder on 28 Oct (options 2 and 3), implemented in this PR and in noema#94:**
- **Trigger (noema#94 and its mirror here):** the receipt's signature must have a non-empty key and a value of 64 lower-case hex characters (what `crypto.Verify` accepts), on both sides (document and row). Tests refuse an empty pair, an empty value, an empty key, non-hex, 63 and 65 characters, a short value, upper case and padded values; each condition fails a test when removed.
- **Controller:** `Controller.completeReceipt` verifies the receipt's HMAC (`KeyRing.VerifyWith` over the canonical JSON of the document it signed) before `Ledger.Complete` writes it, for the receipt of a dispatch (E11) and for the recovery sweeper's receipt. A failed verification returns `ErrReceiptSignature` and writes no receipt, no `RECEIPT` or `GRANT_VOIDED` event, no RecoveryRequest and no outbox event, and the grant is not voided; the attempt stays `DISPATCHING` (from 29 Oct it also appends one `SECURITY_EVENT` `SIGNATURE_INVALID`, see the section below) and the sweeper, with the honest ring, takes it over as `OUTCOME_UNKNOWN`. Tests (controller package and real Postgres): an empty, non-hex, tampered, 63-character, upper-case, well-formed-but-unrelated and valid-signature-of-another-message signature, each for an acknowledged answer and for a stale-version refusal.
- Superseded on 29 Oct: the instruction of 28 Oct was "writes nothing"; the Founders then approved a separate-transaction `SECURITY_EVENT` (D4). See the next section.
- What still holds, stated plainly: the database checks the *shape* of the signature, not the HMAC (it holds no key); the core verifies it once, in the Controller, with the key the Controller holds; whoever can write as `execution_rw` without the Controller can still write a well-formed signature that verifies against nothing. The SoT names Outcome (not built) as a further checker of the receipt's signature (SoT §13.1, "Checks" row).

**Before the decision (kept for the record).** No code path verified the receipt's signature: `KeyRing.VerifyWith` was called only at E1, for the grant; the receipt was signed by `Controller.sign` and written by `ledger.Complete` unchecked; the trigger compared the document's `sig` member with the row's columns and accepted an empty key and value on both sides (shown by a scratch test, not kept).

**Reconciling the two statements.** "The row's signature" is condition (g) of the trigger: the document's `sig` member equals the row's signature columns, and now also has the shape of an HMAC-SHA-256. "The database does not verify the HMAC" is the limit. They do not conflict: (g) is equality plus shape, not a verification; the verification is the Controller's.

**Provenance of "nothing applied".** A confirmed refusal is taken as evidence that no write was applied because a refused conditional write applies nothing (INV-27). That guarantee comes from **mock-case's OpenAPI contract** (`synthetic/mock_case/internal/api/openapi.json`, route response 409 "VERSION_MISMATCH: a distinct error; nothing applied") and from mock-case's own tests, not from a real target's contract: Tushe defines no case API (delta v1.2; worksheet AT-14). **It must be re-verified against any real target before production use.** This is also in the decision record (amendment of 28 Oct, docs#67) and in the migration comment.

## 29 Oct: the SECURITY_EVENT on a failed verification (D4), the `execution_rw` bypass, the D2 assessment

Founder message of 29 Oct (second): step 2 (the role grant) on hold; step 1 (the alert, D4) approved in direction. Authority order applied: SoT, accepted decision records, PERD, plans. Nothing is merged.

### D4: what is built, and the one conflict

- **The conflict.** `DEC-2026-10-28-spent-grant-void` §6.2 (Founders, 28 Oct): a failed verification writes no Record event. PERD v1.18 REQ-SEC-005 ("verification failures fail closed and raise an alert"), §32.2 (Security row, kind `SIGNATURE_INVALID`), EC-X09 ("Reject; security alert") and TM-17 (`SECURITY_EVENT`) call for one. The SoT has no clause that requires it (INV-11 is for chain breaks). The decision record outranks the PERD, so it is amended, not the PERD: [docs#69](https://github.com/Noemai-org/docs/pull/69), a new §7 of the record. PERD and SoT are not edited.
- **Built here** (`Controller.rejectReceipt`, `controller.go`): on a failed verification, in the dispatch path (E11) and in the sweeper, `Ledger.Append` writes exactly one `SECURITY_EVENT` `SIGNATURE_INVALID` in a transaction of its own, and nothing else. Write permission: none new (`record_lock_head`/`record_insert`, already executable by `execution_rw`). Contents: grant ID as the object, `cause` (`RECEIPT_SIGNATURE_NOT_VERIFIED`, or `RECEIPT_DOCUMENT_NOT_CANONICAL`), `receipt_id`, `attempt`. Never the signature, the claimed key ID or the document. If the event cannot be appended, `Execute`/`Recover` return an error that is `ErrReceiptSignature` and carries the append failure; nothing is written either; the attempt stays `DISPATCHING`; the lost event is not invented later. No `ops_alert` table, no metric, no schema field.
- **Tests** (failing first, seen failing before the change; a mutation that drops the append and one that drops the join of the two errors each fail two tests): `TestAFailedReceiptVerificationAppendsExactlyOneSecurityEventAndNothingElse` (controller, simulated, six forgeries; ledger, real PostgreSQL, acknowledged and stale answers: the Record is exactly `REVALIDATION_PASSED`, `GRANT_SPENT`, `DISPATCH_ATTEMPTED`, `SECURITY_EVENT`; kind, `written_by`, object, cause, receipt ID and attempt are read back; the forged value is nowhere in the Record; no receipt, no `RECEIPT`/`GRANT_VOIDED`, no RecoveryRequest, no outbox event; grant `SPENT`, attempt `DISPATCHING`; the sweeper's failed verification adds one more event and the honest recovery adds none; the chain verifies), `TestAnHonestReceiptAppendsNoSecurityEvent`, and `TestIfTheSecurityEventCannotBeAppendedNothingIsWrittenAndBothFailuresAreReturned` (controller, with a refusing ledger; ledger, with a real fault: a trigger planted with the admin role makes `record_insert` refuse a `SECURITY_EVENT`).
- **What it does not do.** It is a Record entry, not a notification: nothing reads it and tells anyone. On the dispatch path the Controller signs the receipt just before it verifies it, so a failure there is a key-ring or canonicalisation fault, not an outside forgery. A repeating key fault adds one event per sweep.

### How a caller with `execution_rw` could, and could not, get a forged receipt accepted

Checked against a real PostgreSQL on 29 Oct (scratch test, not kept): `execution_rw` holds `SELECT, INSERT` on `execution_receipt` and `grant_spend` and `UPDATE (status)` on `decision_grant` (migration 0001, lines 256 to 260).

- **Could.** A caller with `execution_rw` that does not go through the Controller inserts a receipt for a `SPENT` grant that has none: `REJECTED_STALE_VERSION`, `transport_result` `VERSION_CONFLICT_409_412`, no acknowledgement, the spend's tenant, attempt and key, and as its signature any 64 lower-case hex characters, the same in the document and in the row. The guard of `0002` checks the shape of the signature, not the HMAC (the database holds no key), so it accepts the receipt, and `UPDATE decision_grant SET status = 'VOID'` succeeds in the same transaction. The grant is void and no target refused anything. The caller can also insert the RecoveryRequest and the outbox event. A forged receipt written before the real one also wins the unique `(grant_id, attempt)` slot (first writer wins).
- **Could not** (all observed): insert a receipt as `trust_rw`, `outcome_rw`, `evidence_rw` or `orchestrator_rw` (permission denied); insert one for a grant that was never spent (foreign key to `grant_spend`); change or delete a receipt once written (immutability trigger; no `UPDATE` privilege); insert a `decision_grant`, so it cannot invent a grant; void a grant without a receipt, with a receipt of another state, tenant, attempt, key or class, with an acknowledgement, with two receipts, or with a signature of the wrong shape (the `0002` tests); pass the Controller's HMAC check without the key (the Controller path, not a direct insert).
- **Limits of the harm.** The void stops a write; it never causes one (INV-03, INV-17). Outcome reads the target itself by key and does not trust a receipt (INV-04). A retry needs a new Trust ALLOW and a new grant, and the idempotency key holds at the target (INV-07, INV-08). The same credential can call `record_insert` with correctly computed hashes, so the Record is tamper-evident but its writers are not authenticated. A compromised core process holds both the HMAC key and the credential, so the HMAC check does not help there.
- **Why the function would not close it.** A `SECURITY DEFINER` function that checks shape and consistency (step 2 as first proposed) would be called by the same role with the same arguments. It narrows nothing the trigger does not already check. Step 2 is therefore not implemented, and `INSERT` on `execution_receipt` is not revoked: the Controller write path needs it.

### Residual risk, recorded: DEBT-01 ([docs#70](https://github.com/Noemai-org/docs/pull/70))

**Open. Not resolved.** Owner (proposed): SR for closure (task SEC-09), EL for the design decision. Closure criteria: (1) the Founders decide the receipt-signature design and a PERD version and decision record adopt it; (2) a receipt reaches `execution_receipt` only through a verifier that holds a public key and cannot sign; (3) a failing-first test shows a caller with `execution_rw` cannot void a grant with a forged well-formed receipt, by `INSERT` or by any function; (4) the REQ-SEC-005 rotation test passes for the receipt key; (5) Outcome verifies the receipt's signature too (SoT §13.1, OUT-01); (6) EL closes the entry with the evidence. Proposed milestone: a decision or Founder re-acceptance by SEC-09 (Wed 4 Nov 2026), never later than the G3 review (GOV-12, Fri 6 Nov 2026). The Founders set owner and date.

### D2 assessment: asymmetric receipt signatures (design only, no code)

Assumption to confirm: receipts only. Grants and confirmations stay HMAC until Phase 2, as REQ-SEC-005 says; that is an early adoption of REQ-SEC-005's last bullet for one object.

- **Why asymmetric.** With an HMAC, every verifier holds the key that signs. Outcome (named by SoT §13.1 as the receipt's checker) would hold the key and could forge, which breaks the separation of the powers (BD-05). With Ed25519 a verifier holds only a public key.
- **Key handling.** Go standard-library `crypto/ed25519`: no new dependency (AGENT-14); the Python model service never touches it (INV-29). One receipt key pair; the private key readable only by the Execution Controller (file-mounted locally, PLT-05/OQ-09; a regional managed key service before Phase 2, REQ-SEC-005, SoT §47.4); public keys distributed as configuration. `sig.key_id` selects the key and a key registry maps it to algorithm and public key, so no schema field is added (the receipt contract's `Sig` is `{key_id, value}`, `value` minLength 1); an unknown key ID, a retired key or an algorithm that does not match the key ID is refused (no downgrade to HMAC). Rotation as REQ-SEC-005: two active public keys, sign with the newest, retire; `KEY_ROTATED`. The signed bytes stay the JCS canonical document, so the signature is deterministic (INV-21).
- **Verifier placement.** The question is who may accept a receipt.
  - *The database* can verify only if it can check Ed25519 over the canonical bytes. Neither is in the stack: no Ed25519 in the database and no SQL canonicalisation of JCS. A function that checks shape (step 2) authenticates nothing.
  - *The Controller* already verifies; with a public key it cannot sign, but it is the signer's own process, so it adds no separation.
  - *Outcome* (SoT §13.1) is the independent checker the SoT names. Asynchronous, so it detects; it does not stop the void that D2 of 28 Oct puts in the receipt's transaction.
  - *A separate receipt gate service* is a third deployable (SoT §35.6, PERD §36.1): not recommended for the POC.
- **Options.**

  | | Prevents a forged void by `execution_rw`? | Detects it? | What it needs |
  |---|---|---|---|
  | A. Ed25519; the Controller and Outcome verify with public keys | No | Yes, at Outcome (async) | `crypto` signer/verifier split, key registry, vectors, OUT-01 verification step; PERD text for the receipt key |
  | B. A, plus a `BEFORE INSERT` trigger on `execution_receipt` that verifies the signature in the database, with the public keys in a table writable only by the control plane (REQ-SEC-006) | Yes, in the receipt's transaction | Yes | A Postgres extension for Ed25519 (not in PERD §36.1: approval under AGENT-14; none evaluated), the signed canonical bytes stored beside the parsed document so the database verifies exactly what was signed (additive column), the key table, a decision on the key-table writer |
  | C. A, with the void waiting for a verification written by Outcome (`outcome_rw`) | Yes | Yes | Changes D2 of 28 Oct (receipt first, void in a later transaction, a recorded verdict as the precondition); needs OUT-01 first |

  Recommendation: B if the Founders want the bypass closed inside the one transaction they chose; C if they prefer no database extension. A alone is a step, not a closure.
- **Privilege boundaries.** The signer is the Controller (private key); Outcome and the database trigger hold public keys only; the key table is writable by the control-plane role only (SEC-04/REQ-SEC-006), so `execution_rw` cannot add its own key; the private key is never in logs (SEC-07 scanner), the model service or the database. A compromised core process still holds the private key and the credential: only a managed key service (Phase 2) separates them, and it adds an audit log of sign requests.
- **Migration impact.** Forward-only `0003`: replaces the signature condition of the guard (64 lower-case hex becomes the Ed25519 shape, 128 lower-case hex, chosen by the key registry) and, for option B, adds the key table, the verifying trigger and the bytes column, with grants; `schema_test.go` pins change; the `0002` signature-shape tests become verification tests with fixtures signed by a synthetic test key; `docs/db_schema.md` is regenerated. No receipt exists outside tests, so nothing is migrated. A PERD version and a decision record adopt it (REQ-SEC-005, §41, the receipt row of §38.2); the receipt contract needs a migration note (AGENT-15), not a version bump.
- **Test strategy** (failing first). Known-answer vectors (RFC 8032, and the JCS bytes of a receipt); round trip; every member of the document mutated; wrong key, retired key, unknown key ID; algorithm confusion (a 64-hex HMAC under an Ed25519 key ID and the reverse); the REQ-SEC-005 rotation test for the receipt key (old and new accepted in the overlap, old refused after); an architecture-lint rule that the ledger and `outcome/verifier` import only the verify interface; the DEBT-01 closure test (a caller with `execution_rw` inserts a forged well-formed receipt, directly or by any function, and cannot void); the existing forging wrappers (controller and ledger) re-run; mutation checks on each verification step; PERD §47 budget measured, not assumed.
- **Effort and schedule.** A is small. B adds the extension approval, the migration and the database tests. C adds a dependency on OUT-01. These are relative sizes, not estimates. Decision needed by SEC-09 (Wed 4 Nov) to fit before G3.

### OUT-01 (D3) and G1

- **Status: not started.** No open pull request and no branch for OUT-01 (checked 29 Oct, `Noemai-org/noema`); `services/core/outcome/**` holds empty boundary packages (`doc.go`); no `status/OUT-01.md`; `tasks.csv` row `OUT-01`: week 1, due 2026-10-23, owner C2+AG, Needs EXE-02 (done, #47), `current_status` "Unassessed". Part of the predicate language exists (`services/core/internal/predicate`, T6.1). The input is ready: `ledger.Complete` writes the `outcome.verify` outbox event (`status/ENV-03.md`, G2 pre-work: "Outcome (OUT-01, OUT-02) verifies the two receipts the outbox holds").
- **G1 prerequisite.** The G1 gate record ([`gates/G1.md`](https://github.com/Noemai-org/docs/blob/main/docs/poc/execution/gates/G1.md)) does not name OUT-01. Its "Must be true" asks for "The unknown outcome is verified by key, never retried", which only Outcome can do; plan v1.9 (§8 task table) has BEN-05 ("Identical → **G1 evidence**") needing OUT-01 and REC-03, and US-06 ("Silent success") is OUT-01's done-when. So OUT-01 is a prerequisite of G1 by the plan's task graph, not by name in the gate record. The G1 record is "not yet held" (review was scheduled for Mon 26 Oct, 10:00).
- **Remaining work** (plan row): settle window; the verifier's own reads; verify by key; predicate language v1 subset for the slice; verdicts; RecoveryRequest in the same transaction; consuming `outcome.verify`; `VERIFICATION_READ` and `VERDICT` events; US-06 with exactly one dispatch.
- **D2 dependency.** None for G1: verifying by key reads the target and does not use the receipt's signature (INV-04). A receipt-signature check in Outcome (SoT §13.1) should wait for the D2 decision, or Outcome would be built to verify an HMAC it must not hold.
- **Schedule.** The plan date was Fri 23 Oct; it is six days late and G1 waits on it (with BEN-05 and REC-03, whose state I have not assessed). OUT-01 starts only when the Founders assign it. Session A has not started it.

## Messages

### 2026-10-28 (time not recorded) accepted · Relayed by coordinator from founder → Session A
- Purpose: "4. Then start EXE-04, on the merged #87 base. Report the head SHA and run links when it is ready for review. Do not merge. Log every message in status/ENV-03.md." (the full message is in `status/ENV-03.md`).
- Used it for: this task and pull request.
- Evidence: noema#90.

### 2026-10-28 (time not recorded) accepted · Relayed by coordinator from founder → Session A
- Purpose: "#90 is not approved to merge yet. Do not merge. Pause ENV-05 ... Revert the refinement. Restore the original invariant. ... Keep the read through the runtime client. Do not add a truth API handle." (the full message is in `status/ENV-03.md`).
- Used it for: the conflict block above, the reverted simulation files, the oracle note.
- Evidence: noema#90.

### 2026-10-28 (time not recorded) accepted · Relayed by coordinator from founder → Session A
- Purpose: "the founder approves Option 1 in principle for #90, with these conditions. Do not merge #90 until the migration, the specification amendment, the decision record and the regression evidence are reviewed. 1. Docs first ... 2. Noema migration ... 3. Tests ... 4. After the docs amendment merges, re-point the vendored copies ... 5. Report ... Do not merge." (the full message is in `status/ENV-03.md`, added by noema#94).
- Used it for: docs#66, noema#94, and this branch's update.
- Evidence: docs#66, noema#94, noema#90.

### 2026-10-28 (time not recorded) accepted · Relayed by coordinator from founder → Session A
- Purpose: "do not merge #90, #94 or docs#66. Work in this order ... 1. Status mapping ... 2. Receipt proof ... 3. Typed field ... 4. Simulation ... 5. docs#66 ... 6. After docs#66 merges, re-point ..." (the full message is in `status/ENV-03.md`, added by noema#94).
- Used it for: the trigger's receipt checks (noema#94, mirrored here), the e2e mapping assertion, the simulation model and invariants (this PR), docs#66.
- Evidence: noema#94, noema#90, docs#66.

### 2026-10-28 (time not recorded) accepted · Relayed by coordinator from founder → Session A
- Purpose: "the founder decides as follows. Do not merge anything. 1. Receipt signature (blocking) ... a. Tighten the trigger so the signature must be a non-empty hex value. b. Verify the HMAC in the Controller before ledger.Complete writes the receipt ... 3. Status log ... 4. Check runs ..." (the full message is in `status/ENV-03.md`, added by noema#94).
- Used it for: the Controller's HMAC verification and its tests (this PR), the mirrored trigger tightening (noema#94), the relabelling of the entries of this file and of `status/ENV-03.md` in this branch.
- Evidence: noema#90, noema#94, docs#67.

### 2026-10-29 (time not recorded) accepted · Relayed by coordinator from founder → Session A
- Purpose: "the founder's decisions are below ... Step 2 (role grant) is on hold ... Step 1 (alert, D4): approved in direction ... #97: verify ... #90 and #94: verify the current heads, reviews and CI ... D3 / OUT-01: report ... After #97 merges: confirm the docs-drift run ..." (the full message is in `status/ENV-03.md`, added by noema#94).
- Used it for: the D4 code and tests (this PR), docs#69 (the decision-record amendment), docs#70 (DEBT-01), the section above, the check tables on #90, #94 and #97, and the docs-drift run on `main` after #97 ([run 68](https://github.com/Noemai-org/noema/actions/runs/40865302042), success).
- Evidence: noema#90, noema#94, noema#97, docs#69, docs#70.

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

# Session 2026-10-29 12:30 WAT · EXE-04

## Session summary
- What changed (PR links): noema#90: `Controller.rejectReceipt` (one `SECURITY_EVENT` `SIGNATURE_INVALID` on a failed receipt verification, in a transaction of its own), `controller.Event.Payload`, `Ledger.Append` uses it, the cause constants; READMEs and the migration comment reworded ("writes no receipt", mirrored in noema#94). docs#69 (DEC-2026-10-28 §7), docs#70 (DEBT-01).
- Why: Founders' decisions of 29 Oct: D4 (alert) approved in direction; step 2 (role grant) on hold, the bypass recorded as a residual risk; D2 assessed; OUT-01 reported.
- Risks, and how they are contained: a P0 path (Execution, Record). The new write is one Record event in its own transaction, on a path that otherwise writes nothing; if it fails, nothing else is written and both errors are returned. No new privilege, table, metric or schema field. The bypass by `execution_rw` is not closed: DEBT-01.
- Tests added (REQ, INV, US or EC IDs): `TestAFailedReceiptVerificationAppendsExactlyOneSecurityEventAndNothingElse`, `TestAnHonestReceiptAppendsNoSecurityEvent`, `TestIfTheSecurityEventCannotBeAppendedNothingIsWrittenAndBothFailuresAreReturned` (controller and ledger; REQ-SEC-005, REQ-AUD-001, EC-X09, EC-E16, TM-17).
- Regression cases added: the three above (each seen failing before the change).
- Debt entries: DEBT-01 (docs#70), proposed, open.
- Conflicts raised: DEC-2026-10-28 §6.2 against PERD v1.18 REQ-SEC-005, §32.2, EC-X09, TM-17; resolved by the proposed §7 (docs#69), awaiting the Founders.

## Pre-work for the next agent
- Exact next step: the Founders' decisions on docs#69, docs#70 and the D2 design; then, if D2 is approved, a PERD version and decision record first, then `0003`.
- Open questions (owner): D2 option A, B or C and the owner and date of DEBT-01 (Founders); whether OUT-01 starts now (Founders).
- Files to read first: `services/core/execution/controller/controller.go` (`completeReceipt`, `rejectReceipt`), `services/core/execution/ledger/receipt_signature_test.go`, the D2 section of this file.
- Do not touch: `decision_grant_guard` (migration 0002 only); the grants of `execution_rw` on `execution_receipt` (the Controller path needs them); any threshold, TTL or retry budget.
