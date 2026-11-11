# Status · EXE-08 (Core Engineer 1) · W4, database-side verification of receipts

Updated 2026-11-05. Plan task EXE-08: owner C1; REQ-AUD-006, REQ-EXE-004; Needs EXE-07 (slice 1 merged in noema#129) and SEC-12; done when failing-first forgery, replay and downgrade tests pass and noema#94 and noema#90 can be released.

## State: slice A merged (noema#178). Slice B (this section, below) is a Draft stacked on the CI job of noema#214. EXE-08 is not Done.

## Slice A (this session): ownership and privileges, no extension
- Migration `0010_record_tables_to_migrator.sql`: the Record tables belong to `noema_migrator`; `record_append` holds grants, not ownership, and loses `CREATE` on `public` (DEBT-05). D14, D15, D16 and §6.1 of DEC-2026-11-01-receipt-and-record-signing.
- Tests (`platform/db/ownership_test.go`, written first and seen failing): the tables' owner; no runtime role holds `CREATE` on any schema or on the database; no runtime role is a member of `noema_migrator` or has a privileged attribute; `record_append` cannot disable a trigger on, alter, drop, truncate, update, delete from or shadow around the Record tables.
- The test cluster now creates `noema_migrator` and gives `record_append` `CREATE` on `public` before migrating, exactly as `init.sh` does, so the pin can fail.
- `deploy/compose/postgres/test_init.sh` (which CI does not run) was broken by noema#145's two new secrets; fixed, extended to apply every migration as the plain migration login (it works on PostgreSQL 16 locally) and to check the owner and the revoked `CREATE`.

## Not in slice A, and why
- **pgsodium, the custom image, `noema_sig.verify`, `signing_key`, the receipt trigger, `receipt_insert`, `execution_receipt.signed_jcs` (D10 to D13, tests 1 to 6, 8 to 16).** OD-P-72 (adopting pgsodium, AGENT-14) is still `[OPEN]` in `docs/open-decisions.md` and DEC-2026-11-01 still reads `Proposed`, though docs#80 is merged. "Nothing is built from this entry until the Founders approve it." Needs the Architect's or Founders' confirmation that the dependency is approved, and a committed record of it.
- **`cmd/migrate` and the one-shot `migrate` service (D14a, test 18)**: next slice; it touches `deploy/compose` and the core image.

## Pre-work for the next agent
- Exact next step: slice B once OD-P-72 is recorded as approved: the image (digest-pinned base, libsodium by SHA-256 and signature, pgsodium by commit), the bootstrap with the event trigger disabled, the guards in `db.Migrate` and core start-up, then the migration with `signing_key`, `noema_sig.verify`, the receipt trigger and `receipt_insert`.
- Open questions (owner): is OD-P-72 approved (Architect/Founders; the record is still `Proposed`); wire `test_init.sh` into the `compose` job (needs a `.github` change, a founder approval).
- Files to read first: `docs/decisions/DEC-2026-11-01-receipt-and-record-signing.md` §5, §6, §10.
- Do not touch: noema#94, noema#90 (held until W4 is on `main`).

## Session 2026-11-06 · EXE-08 slice B: database-side verification of receipts (DEBT-01, Option B)
### Session summary
- What changed (PR links): the slice B pull request on `feat/EXE-08-sig-db`, STACKED on noema#214 (the CI job, ruling A16). Do not merge before docs#117 (rulings A2 and A16).
  - Migration `0013_receipt_signature.sql`: schema `noema_sig`; `signing_key` (K1: `key_id = identity.ed25519.version`; at most two ACTIVE per identity; a newer version each time; only ACTIVE to RETIRED or REVOKED, REVOKED keeps `compromised_since`); `key_register`, `key_retire`, `key_revoke` (the only way to change it; no runtime role can execute them); `noema_sig.verify` (D11, `SECURITY DEFINER`, fixed `search_path`, no exception handler); `signed_jcs` on `execution_receipt`; a `BEFORE INSERT` trigger, `ENABLE ALWAYS`, that checks the stored document is the signed one, every bound field equals the row's column, tenant, attempt and idempotency key equal `grant_spend`, and the signature verifies over the tag `noema/receipt/v1` plus the signed bytes, else `NS001`; `receipt_insert` (the only insert path; `execution_rw` loses `INSERT`); every object owned by `noema_migrator` and every function schema-qualified.
  - `db.Migrate` refuses while an event trigger of pgsodium is enabled (D10a). `init.sh` creates the extension once, disables its event triggers, sets the setting off, and revokes everything from PUBLIC. Compose builds the pinned image (`deploy/spike/pgsodium/Dockerfile`; moving it out of `spike` is a follow-up that also repoints the CI job).
  - `ledger.Complete` stores through `receipt_insert`; the Controller signs with an Ed25519 `ReceiptSigner` (`signing.Ring.SignReceipt`, EXE-07 slice 1) and no longer holds a receipt key ID. Grants stay HMAC.
  - Test infrastructure: `testpg` bootstraps pgsodium in every test database when the extension is installed; `internal/sigfixture` (no import of Execution, so Outcome's tests stay independent) and `execution/signing/signingtest` make SYNTHETIC keys and signed receipts.
- Why: DEBT-01 (Tier 0). A holder of `execution_rw` could insert a forged `REJECTED_STALE_VERSION` receipt (any 64 hex characters as a signature) and the SPENT-to-VOID guard of noema#94 would have trusted it.
- Risks, and how they are contained: verification fails closed (no extension, a library that cannot load, the function raising: the insert fails and the transaction keeps nothing; tested both ways). The private key is never in PostgreSQL. A compromised core process still holds the signing key (named in DEC-2026-11-01 section 7). Existing receipt-writing tests now sign their receipts.
- Tests added (REQ-AUD-006, REQ-EXE-004, INV-04, INV-27), written first and seen failing (`key_register(...) does not exist`): `platform/db/sig_test.go`, one per closure check of SR: a genuine receipt is stored; a forged receipt is refused by every path (receipt_insert, direct INSERT, owner INSERT under `session_replication_role = replica`); replay onto another grant, tenant, attempt, key, state, receipt or acknowledgement; a genuinely signed receipt that contradicts the spend; rotation (two ACTIVE keys, a third refused, a retired key refused, an old version refused); a revoked key and `compromised_since`; another identity's key; verification unavailable (extension dropped, function raising); no runtime role can alter, drop, disable, replace or bypass the control or touch `signing_key`; no runtime role is a member of `noema_migrator` or holds a privileged attribute; the table keeps K1 and D5 for the owner too; the purpose binds to the identity in the verifier; `Migrate` refuses an enabled event trigger; and the DEBT-01 closure test on the held noema#94 guard (testdata), with a negative control in which the trigger is off and `INSERT` is granted, where the same forgery voids the grant.
- Mutations run and reverted (all caught unless named): status unchecked; spend match removed; trigger not ENABLE ALWAYS; `INSERT` not revoked; purpose-identity binding removed; the event-trigger guard removed. Not reachable by any test, stated: the verifier's parse of the key ID against the row (the table's own constraint already makes them equal).
- Regression cases added: the tests above. Debt entries: none new. Conflicts raised: none.
- Local evidence: PostgreSQL 16.15 with pgsodium v3.1.11 and libsodium 1.0.22 built from the pinned sources, `go test ./...` and `deploy/compose/postgres/test_init.sh` pass; the CI job runs 17.11 (ruling A16). The red run: the red run (every sig test failed on `function key_register(unknown, unknown, unknown) does not exist` with 0012 removed).

### Not built in this slice (named)
- `cmd/migrate` and the one-shot Compose `migrate` service (D14a): migrations still run as the bootstrap superuser in Compose. The core's start-up check for the extension and the event trigger (D10a, D13) is not wired in `cmd/core`.
- Controller verification of the receipt's Ed25519 signature (noema#90's step) and the `SECURITY_EVENT` on `NS001` (D12): noema#90 is held.
- The Outcome side and the Record events (REC-08, DEBT-03): the trigger for `record_event` and the key table's other identities.
- noema#94 and noema#90 are not touched; their release order is in their PR bodies.

## Pre-work for the next agent
- Exact next step: SEC-13 (the Security Reviewer) validates the six closure checks on the final head; then noema#94 is merged `main` in, renumbered after this migration, its condition (g) changed to "the receipt row exists", and its shape tests turned into verification tests (Track 2).
- Open questions (owner): where `cmd/migrate` and the start-up check land (Architect).
- Files to read first: `services/core/migrations/0013_receipt_signature.sql`, `services/core/platform/db/sig_test.go`, `docs/decisions/DEC-2026-11-01-receipt-and-record-signing.md` D4 to D16.
- Do not touch: noema#94, noema#90 (held, DEBT-01).

# Session 2026-11-06 15:00 WAT · EXE-08 (DEBT-01 slice C: #94 continuation)

## Session summary
- What changed: continuation of noema#94 (not pushed to: authored by the other account). Branch = #229 (slice B) + #94 merged; `0002_grant_spent_void.sql` renumbered `0013_grant_spent_void.sql`; condition (g) is "receipt row exists" (signature verified by 0012); grant-void tests use genuinely signed receipts (permitted path) and a trigger-off world for the guard-only refusals; the two SR-flagged main tests now run the real 0013 file (`TestUpgradeWhereTheGrantVoidMigrationArrivesAfterOutcome`, `TestTheGrantVoidMigrationAppliedLateGivesTheSamePrivilegesAsAFreshDatabase`).
- Why: SR closure of DEBT-01 (a forged receipt must not void a spent grant); the old 0002 applied after 0012 would have replaced a verified world with a shape-only guard.
- Risks: merge order. Release order: docs#117, #229, this PR; #94 is superseded by this PR and must not merge first. Number 0013 may need D3 renumbering if another migration lands first.
- Tests added: grant_void_test.go (REQ-EXE-004, INV-27, INV-03), the two upgrade tests; TestAForgedReceiptCannotVoidASpentGrantOnTheHeldGuard (slice B) is the closure test on the held code.
- Debt entries: none new. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: Codex review at the final head once CI is green; founders decide the merge order.
- Do not touch: `services/core/migrations/testdata/0002_grant_spent_void.noema94.sql` (pinned).

## Update 2026-11-07 (Founder decision D12: migration numbers)
- `0013_receipt_signature.sql` (noema#229) and `0014_grant_spent_void.sql` (this PR) replace the provisional `0012` and `0013` named in the sessions above. 0012 is noema#168's, 0015 noema#259's, 0016 noema#234's, 0017 noema#261's, 0018 noema#263's. If the merge order changes, the later PR renumbers.

## Session 2026-11-06 (later) · ruling A24: the Ledger's rollback is bounded
- `execution/ledger` rolled back with `context.WithoutCancel(ctx)` and no bound, so a server that did not answer would hold the step forever. It now uses `context.WithTimeout(context.WithoutCancel(ctx), RollbackTimeout)` (`rollbackBounded`), and a Ledger with no `RollbackTimeout` refuses to start a transaction (no default; the value comes from configuration). Tests first: a hung rollback is abandoned after its bound and was tried although the caller's context was cancelled; an answered rollback returns at once; a Ledger without a bound refuses. Mutations caught: no timeout; the caller's cancellation inherited. The other rollbacks in `record/reconstruct`, `record/verify` and `platform/db` already carry a bound; `outcome/store` is noema#202.
## Session 2026-11-06 (later) · ruling A24: the Ledger's rollback is bounded
- `execution/ledger` rolled back with `context.WithoutCancel(ctx)` and no bound, so a server that did not answer would hold the step forever. It now uses `context.WithTimeout(context.WithoutCancel(ctx), RollbackTimeout)` (`rollbackBounded`), and a Ledger with no `RollbackTimeout` refuses to start a transaction (no default; the value comes from configuration). Tests first: a hung rollback is abandoned after its bound and was tried although the caller's context was cancelled; an answered rollback returns at once; a Ledger without a bound refuses. Mutations caught: no timeout; the caller's cancellation inherited. The other rollbacks in `record/reconstruct`, `record/verify` and `platform/db` already carry a bound; `outcome/store` is noema#202.

## Session 2026-11-11 · EXE-08 slice B (noema#229): after #168 merged, test-seed fix
- What changed (PR links): noema#229: two test files, no production code. `platform/db/sig_test.go` seeds a `decision_record` before each grant and gives the grant's document the decision, the tenant and the bound fields (as `schema_test.go` does), because the grant guard of 0012 (#168) refuses a grant without them; the "void without a receipt" check runs through the tenant's own execution login, because row security hides a grant from an unmapped session. `adapters/outcome_e2e/crash_test.go` (#252, on `main`) builds the Controller with the Ed25519 receipt signer (`Receipts: signingtest.Ring`) instead of the removed `Keys: receiptRing` and `ReceiptKeyID`.
- Why: with #168 on `main`, 14 receipt tests failed (the seeds) and `outcome_e2e` no longer compiled (`crash_test.go` used the pre-#229 Controller fields).
- Tests: red first, locally, after merging `main`: the 14 tests of `sig_test.go`, then the build failure of `outcome_e2e`; green after: `go test -count=1 ./...` in `services/core`: 37 packages ok, 0 failing.
- Debt entries: none. Conflicts raised: none.
- Mutation check of 0013 (A131 row 3c), same session: four mutants. The spend cross-check removed fails `TestAGenuinelySignedReceiptThatContradictsTheSpendIsRefused`; a non-ACTIVE key accepted fails the rotation and revocation tests; the key-change guard setting removed fails `TestTheKeyTableKeepsItsInvariantsForTheOwnerToo`. **The "stored document is the signed one" check removed was not caught by any test**, so `TestAGenuineSignatureOverADifferentStoredDocumentIsRefused` is added (an extra member in the stored json, or another signature member, over a genuine signature: refused with NS001); it passes on the migration and fails on that mutant. Test-only; the migration is unchanged.

## Session 2026-11-11 · EXE-08 slice C (noema#233): after #181, #168 and (stacked) #229's current head
- What changed (PR links): noema#233, test-only: `platform/db/grant_void_test.go` maps the test session to `ten_a` after `seed` (`seedMapped`), because since #168 row security hides a grant from a session with no tenant mapping, so the raw `SET ROLE execution_rw` statements updated no row and met no guard (7 tests); its helper grant carries a document that names the decision and tenant and the bound fields, as #168's guard requires; `platform/db/migration_order_test.go` is the same upgrade-path change as in #229 and #234 (identical content, so the three merge cleanly). `migrations/README.md` is main's file (A145). Merged #229's current head (its test seeds and the crash child's receipt signer).
- Why: with #181 and #168 on `main`, 11 tests of this branch failed.
- Tests: red first, locally: `TestAGenuinelySignedAcknowledgementDoesNotVoidASpentGrant`, `TestASpentGrantStaysSpentWithoutAConfirmedStaleVersionRefusal` and the other void/forged-receipt cases ("got nil, want an error with code 23*": the guard was not reached); green after: `go test -count=1 ./platform/db/ ./outcome/... ./adapters/outcome_e2e/ ./execution/... ./evidence/...`.
- Debt entries: none. Conflicts raised: none.
