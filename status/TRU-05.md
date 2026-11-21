


# Session 2026-11-08 · TRU-05 slice 2 (noema#168)

## Session summary
- What changed: the Architect's ruling of 8 Nov 2026 on noema#329, all four rules in the database. (d) a CHECK `decision_record_bound_fields`; (a) to (c) in `decision_grant_requires_allow`. Fixtures that insert records and grants directly now carry the bound fields (`testpg.BoundFieldsSQL`, `testpg.GrantBoundSQL`, `testpg.DecisionDocJSON`).
- Tests added (REQ-TRU-008, INV-17): four, in `platform/db/grant_matches_record_test.go`, seen failing before the migration change.
- Risks: a grant fixture that omits the intent hash, policy version or a valid expiry is now refused; every existing fixture was updated and the whole `services/core` suite passes locally.
- Not done: SR's note 5 (owner policy under a non-superuser owner stays unproven). #168 needs a fresh reviewer READY at the new head.

## Pre-work for the next agent
- Branches stacked on #168 (#197, #237, #261, #233) insert records or grants directly in tests: merge this head in and add the bound fields.

# Status · TRU-05 (Decision Records before grants; HMAC grants over JCS; two active keys)

State: In progress (slice 1 built). Needs REC-01: merged (noema#122, 4 Nov 2026 12:58 UTC), so the task started the same day.

## Session 2026-11-04 · slice 1: grant issuance, the JCS HMAC, key rotation
## Session summary
- What changed: `services/core/trust/grants/{grants.go,grants_test.go,doc.go}`.
- Why: REQ-TRU-008, REQ-SEC-005, REQ-TRU-001; INV-03, INV-25. TRU-05 is the G1 blocker I own (the US-01/US-06 write path needs a grant).
- What it does: `Issuer.Issue(decision, step)` returns the grant for one write plan step of an ALLOW decision with a complete basis, and nothing for any other outcome or an ALLOW without its basis (`ErrNotAllow`). It binds tenant, correlation, decision, principal, intent hash, capability, target, params hash, evidence-set hash, plan hash, plan step, target versions, policy version (which must be one the decision's basis names, INV-09), pack version, `not_before` = the evaluation time, `expires_at` = + the configured validity, a 128-bit CSPRNG nonce, `single_use`, and an optional confirmation event. The shapes of the identifiers are checked against the contract's patterns before anything is signed (the generated Go types check presence, not form). `Keys` holds one or two keys: the first signs, any verifies; a retired key is absent, so its grants are rejected. HMAC-SHA-256 over the JCS form of the grant without `sig`, with `key_id`. An entropy failure is an error, never a panic.
- Tests added (REQ-TRU-008, REQ-SEC-005, INV-03, EC-X09): all-field binding; the six non-ALLOW outcomes, a zero decision, an ALLOW without a basis and one with a minimal basis issue nothing; ten malformed steps refused; 500 grants with distinct IDs and nonces and an entropy failure as an error; a change to any of 23 fields breaks the signature; rotation (old and new key both verify during the overlap, the old key is rejected after retirement, a claimed key that did not sign is rejected); the key set refuses none, three, short secrets, an empty or repeated ID and a shared secret; the signature is over the canonical JSON and a re-spelled equal grant verifies; tamper fuzzing (20,000 seeded byte changes, 0 verified). Seen failing first (did not compile). Mutation: 12 mutants, 11 caught at first, the survivor (the basis check) got a test, then 12 of 12.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Not built (named; TRU-05 is not Done)
- **The `decision_record` table and the durable write before any grant.** The table does not exist (migration `0001` says the Trust tables arrive later), and the migration numbers 0004 to 0006 are claimed by noema#145 and 0004 by noema#136. I need a migration number and the Steward's or Architect's confirmation of which comes first before I write it. (Answered: 0009, in noema#168; noema#167 keeps 0008, ruling of 5 Nov 2026.)
- The database trigger that forbids a `decision_grant` row whose decision is not ALLOW (needs the table).
- Grants persisted in the same transaction as their `GRANT_ISSUED` events, and the `KEY_ROTATED` event (need the Record writer wiring).
- The key source: keys come from a file-mounted secret in `cmd/core` (OQ-09 default); loading is not built. The validity (10 minutes, ADOPTED) is a constructor input.
- Policy version (**ruling D1 of 5 Nov 2026, proposed in docs#112 and authoritative only once docs#112 merges (AGENTS.md §2.3); it replaces the Architect's session decision of 4 Nov**, which is superseded and must not be restored; this PR must not merge before docs#112): a grant binds the exact `policy_id@version` it was decided under. Versions are compared only within one policy_id (dotted numbers, 3.10 newer than 3.9); nothing is ever ordered across policy IDs, and there is no "newest across policies". `Step.PolicyID` names the policy; if the basis names one policy it may be omitted, and if it names several and none is named, or the named one is absent, no grant is issued. At execution, E4 voids the grant (`VoidPolicySuperseded`, a RecoveryRequest returns the case to Trust) if the active version of the grant's policy_id differs, is absent, or the grant's `policy_version` does not match the contract pattern; another policy's version never voids a grant. TRU-08's void on publication reads the grant's own policy_id and version, not the whole basis. Tests were written first; mutants of `boundPolicy` and of the E4 check are caught.
- Migration number (decided, 4 Nov 2026): `decision_record` is migration **0009** (noema#168): noema#167 (EVD-01) keeps 0008, ruling of 5 Nov 2026. The table and trigger are written there; the Go write path is not.

## Pre-work for the next agent
- Exact next step: the `decision_record` migration (after the number is settled), the trigger, the Record-first write in one transaction with the grants, and the 2-key rotation test against the database.
- Files to read first: `trust/grants/grants.go`, PERD REQ-TRU-008 and REQ-SEC-005.
- Do not touch: `noema#94`, `noema#90`.

## Session 2026-11-05 (evening) · TRU-05 slice 1 follow-ups from Codex
- A grant now needs a `reconcile.Permission` bound to the step's capability **and target** (`FreezeGate.PermitTarget`, additive in `trust/reconcile/freeze.go`; `Permit` is unchanged and yields a permission that names no target, which opens no grant). The `frozen` flag a caller passes comes from the reconciliation result for that same target; the gate does not read freeze state itself. The pack version of the basis is checked against the grant contract's pattern before signing. Both found by Codex at `9dbb363` and `4546400`; tests first, mutants caught.
- Touches `trust/reconcile` (T6.2, another task) additively; the existing `Permit` tests in `internal/corpus` are unchanged and pass.

## Session 2026-11-06 · TRU-05 slice 3a (signing a Decision Record v2), Engineering Lead
- Added `Keys.SignRecord` and `Keys.VerifyRecord` (`trust/grants/record.go`): HMAC-SHA-256 with the grant keys over the domain label `noema/decision-record/v2`, a newline, and the JCS form of the record without `sig` (ruling A5; the contract is `decision-record.v2` in noema#168). Only major 2 is signed. Tests first (did not compile): sign and verify; a change to any of six fields breaks it; an inactive key is refused; the signature is not valid over the bare JCS bytes, and is over label, newline and JCS. Mutants without the label, without the newline and accepting v1 are caught; skipping the version error on verify is an equivalent mutant.
- **Stacked on noema#163 (grants) and noema#168 (the `decision_record` table and the v2 contract).** Do not merge before both. Rulings A5 and A6 are not authority until their decision record is committed.
- **Not built, and why — DECISION NEEDED before the writer (item 3 of the 6 Nov order):** a Decision Record v2 requires `evidence_freshness[]` items with `source`, `age_seconds`, `effective_limit_seconds` and `limit_set_by`, but `decide.Basis.EvidenceFreshness` carries only `EvidenceID` and `EffectiveAt`. The record cannot be built from a `Decision` as it stands, and TRU-13 says the basis is built by the gate steps from what T9 and T11 read, never supplied by the caller. Recommendation: extend `Basis`/`Freshness` with those four fields, set by the gate steps (TRU-13), then the writer builds the record from the decision alone. Also open: `intent_id`, `correlation_id` and the minted `decision_id` come from the caller's context, not from `Decision`.
- Then: the transaction (record, then grants, `GRANT_ISSUED` per grant), `KEY_ROTATED`, key loading from the file-mounted secret (OQ-09 default).

## Session 2026-11-06 (later) · TRU-05 slice 3a revised to ruling A13
- A13 replaced my reading of A5: the signed bytes are the label `noema/decision-record/v2`, **one 0x00 byte** (not a newline), then the JCS bytes of the record without `sig.value`; `sig.key_id` stays in them. The bound fields are `intent_id`, `correlation_id`, `policy_id@version`, `issued_at`, `expires_at` (simulated clock) and `key_id`; a record missing any is invalid. `decision-record.v2` (noema#168) now requires `issued_at` and `expires_at`; `SignRecord` refuses a record that lacks a bound field, has a malformed time, or whose `expires_at` is not after `issued_at` (`ErrRecordIncomplete`).
- Tests first (red at the previous commit): the message is label, 0x00, JCS with the key_id; a newline separator and a message without the key_id do not verify; nine incomplete or empty-window records are refused. Mutants (newline, no key_id binding, intent_id check dropped, signature value left in the message) are caught; a mutant that drops the window check was checked by a compile-clean variant.
- A13 and A5 are not authority until their decision record is committed (docs#117 carries A2 to A12; the record for A13 to A23 is not one I have seen). The Decision Record writer is still blocked on the evidence-freshness fields of `decide.Basis` (see the earlier section).


# Session 2026-11-08 · TRU-05 slice 2 (noema#168)

## Session summary
- What changed: the Architect's ruling of 8 Nov 2026 on noema#329, all four rules in the database. (d) a CHECK `decision_record_bound_fields`; (a) to (c) in `decision_grant_requires_allow`. Fixtures that insert records and grants directly now carry the bound fields (`testpg.BoundFieldsSQL`, `testpg.GrantBoundSQL`, `testpg.DecisionDocJSON`).
- Tests added (REQ-TRU-008, INV-17): four, in `platform/db/grant_matches_record_test.go`, seen failing before the migration change.
- Risks: a grant fixture that omits the intent hash, policy version or a valid expiry is now refused; every existing fixture was updated and the whole `services/core` suite passes locally.
- Not done: SR's note 5 (owner policy under a non-superuser owner stays unproven). #168 needs a fresh reviewer READY at the new head.

## Pre-work for the next agent
- Branches stacked on #168 (#197, #237, #261, #233) insert records or grants directly in tests: merge this head in and add the bound fields.

# Session 2026-11-08 · TRU-05 signature domain separation

## Session summary
- What changed: ruling of 8 Nov 2026 on noema#292. A grant signature covers `noema/decision-grant/v1`, a 0x00 byte, then the JCS bytes; the confirmation request gets its own label, `noema/confirmation-request/v1`, in `platform/crypto` for the confirmation signer to use. Trust's issuer, Execution's E1 and every test signer use `crypto.GrantSigningMessage`. Golden vectors 1.0 to 1.1 (`grant_signing_vectors`: new value, `domain_label`, `must_not_verify`), with the migration note in `contracts/fixtures/README.md`; the independent Python checker (`tools/check_vectors.py`) agrees with the Go value.
- Tests added (REQ-TRU-008, INV-03): `TestGrantSigningVector` (rewritten), `TestAGrantSignedOverBareJCSOrUnderAnotherLabelIsRefused`, two E1 cases in `TestEachCheckVoidsTheGrantWithItsReason`. Seen failing first.
- Risks: P0 path and a contract fixture change (AGENT-15); both Founders approve. No stored grant exists, so nothing migrates. Stacked on noema#163.
- Not done: the confirmation signer does not exist yet; this PR only reserves its label and message function.

# Session 2026-11-11 · TRU-05 slice 3b (the Decision Record writer, noema#237), definition of done

## Session summary
- What changed (PR links): noema#237. Merged `main` (grant signing label from noema#337; own recorder diff unchanged, patch-id `e4f65676`). Removed `Request.Actor` (Codex P1: decision events are written as `core-trust`, INV-12). Added tests for the error paths of `Write` (second human review P1, C1): a failing DECISION_MADE append, a failing GRANT_ISSUED append, a duplicate decision id at the `decision_record` insert and a duplicate grant id at the `decision_grant` insert; each error was swallowed in turn and exactly its test went red. Added a test that a Request naming another tenant is refused by the database. All tests build the Appender through `newAppender`, the one line to change when noema#263 makes signed appends mandatory.
- Why: Record-first (PERD 32.4, EC-E16): a failed append or insert must fail the whole write.
- Risks, and how they are contained: noema#263 (REC-08) and noema#410 (EVD-02) land in the same area; whichever merges second needs `Signers` from `recordsig` in `newAppender`, and the grant insert may need the evidence-set hash. Neither package exists on `main` yet, so I did not guess at them.
- Tests added (REQ-TRU-001, REQ-TRU-008, INV-03, INV-12, INV-17): `TestARequestCannotChooseTheRecordActor`, `TestAFailedDecisionMadeAppendFailsTheWriteAndLeavesNothing`, `TestAFailedGrantIssuedAppendFailsTheWriteAndLeavesNothing`, `TestAFailedDecisionRecordInsertFailsTheWriteAndLeavesNothing`, `TestAFailedGrantInsertFailsTheWriteAndLeavesNothing`, `TestARequestNamingAnotherTenantIsRefusedByTheDatabaseAndLeavesNothing`.
- Regression cases added: the same tests.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after noema#263 merges, set `Signers` in `newAppender` (recorder_test.go) from `recordsig.For` for `core-trust`.
- Open questions (owner): C1's non-blocking notes 2 to 5 on noema#237 (row counts after refusals, a WAIT/CONFIRM/UNKNOWN record, an empty `policy_id` in the DECISION_MADE payload, idempotency of a retried `Write`): the caller or workflow layer owns idempotency (TRU-08 or EXE-06), EL to confirm which.
- Files to read first: `services/core/trust/recorder/recorder.go`, `recorder_test.go`.
- Do not touch: `trust/grants` signing (noema#337).

## Session 2026-11-12 18:00 WAT · TRU-05 (fixture signers, merge main)

## Session summary
- What changed: merged `main` (clean; `#263` REC-08 and later are on `main`); `newAppender` in `trust/recorder/recorder_test.go` now takes the fixture signer for `core-trust` from `internal/recordsig` (SYNTHETIC key, made per test), as the second human review asked. Test file only; `recorder.go`, `doc.go` and `trust/grants/record.go` are unchanged.
- Own diff against `main`: 5 files, +925. Patch-id of the non-test sources before and after the merge: `1d17245f8cd62f81`.
- Tests: the existing recorder tests now run with a signed appender. The tests that append signed Record events cannot run on my machine (no `pgsodium`); CI is their first run at this head.

## Pre-work for the next agent
- Exact next step: none for this PR once reviewed; CI at the head must be read before READY.
- Do not touch: `Write`'s order (Record-first).

## Session 2026-11-13 17:00 WAT · TRU-05 follow-up (A172, A186): one decision per full input tuple

## Session summary
- What changed (PR: the pull request of branch `feat/TRU-05-decision-input-tuple`; it replaces noema#527, closed because a secret-scanner false positive sat in one of its commits and history is never rewritten): migration `0027_decision_input_key.sql` (unique index `decision_record_input_key` over the tenant, the intent ID, the intent hash, the context snapshot and the policy version); `trust/recorder`: `Writer.Write` takes an advisory transaction lock on the tuple, reads the stored decision, and returns it with its grants and writes nothing; a stored decision that the new evaluation contradicts is `ErrDecisionConflict`.
- Why: ruling A172 (the follow-up of noema#237, Tier 1, failing test first, C1 reviews) and ruling A186 (Founder-approved interim, relayed, not yet committed): the same full input tuple gets the stored decision; a new snapshot makes a new decision. A retried or resumed run reads the decision instead of deciding again, and a second grant cannot be issued for one input (INV-03, INV-21, INV-25).
- Key mapped to the real columns: `tenant_id` and `intent_hash` are columns; `intent_id`, `evidence_snapshot_id` and `policy_version` are members of the signed document. A document with no snapshot (only direct test fixtures) is NULL in the index and not keyed.
- Risks, and how they are contained: a duplicate tuple already in a database makes the migration fail (nothing is deleted; a person looks); a hit that disagrees (outcome, disposition, rule versions, grants' step, capability, target, params hash, plan hash) is refused, not returned.
- Tests added (REQ-TRU-007, REQ-TRU-008, INV-21, INV-25): `platform/db/decision_input_key_test.go` (2), `trust/recorder/input_key_test.go` (5). `TestAFailedDecisionRecordInsertFailsTheWriteAndLeavesNothing` now collides the decision ID with two inputs (two intent hashes) because an identical request now returns the stored decision.
- Regression cases added: none beyond the above. Debt entries: none. Conflicts raised: none.
- Not done, stated: no production caller of `Writer.Write` exists yet; the `orchestrator.Trust` adapter that calls it is a later PR. A WAIT decision re-evaluated with an unchanged snapshot returns the stored WAIT (A186 as written): a re-evaluation must build a new snapshot (open point 1 on noema#527).

## Pre-work for the next agent
- Exact next step: the `orchestrator.Trust` adapter (evaluate, then `Write` in one transaction) once C1's Trust service assembles T1 to T15; resume of a stranded run (OD-P-85, A168) reads the stored decision through `Write`.
- Open questions (owner): whether a WAIT re-evaluation of an unchanged snapshot should be a new decision (Architect: it would put the clock reading in the key).
- Files to read first: `trust/recorder/inputkey.go`, `migrations/0027_decision_input_key.sql`.
- Do not touch: `Write`'s order (Record first on a miss; nothing appended on a hit).
