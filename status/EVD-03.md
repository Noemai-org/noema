# Status · EVD-03 (Core Engineer 2) · sealed context snapshots

Updated 2026-11-07. Plan task EVD-03 (owner C2; REQ-CTX-004). Slice 1 (the hash) is noema#225; slice 2 (this file's section) is one PR on `main`.

# Session 2026-11-07 · EVD-03 (slice 2)

## Session summary
- What changed: migration `0019_context_snapshot.sql` (number provisional, see noema#183); `deploy/compose/postgres/init.sh` creates the `evidence_verify` login with its own secret (`evidence_verify_password`; compose, `secrets_local.sh`, README, `test_init.sh`, `assert_roles.sql`); `testpg` gains the cluster login and `EvidenceVerifyLogin`; `evidence/snapshot/{seal.go,recompute.go}` and their tests. `hash.go` and `hash_test.go` are the identical files of noema#225, so the two PRs merge in either order.
- Why: Security Reviewer's acceptance on noema#225 (conditions on comment 6708837337) and D9. The database recomputes the hash on insert; the nightly job recomputes it again as a login that can read two tables and write nothing.
- Risks: a new role and a new migration on the P0 path (Record, contracts). Contained by: no membership, SELECT on exactly two tables, one SELECT policy each, forced RLS; the catalog test pins the grants; `test_init.sh` proves them on the Compose bootstrap.
- Tests added (REQ-CTX-004, INV-17, INV-11, REQ-SEC-004, REQ-AUD-001, D9): SQL hash equals the golden vectors; honest snapshot stored; ten dishonest ones refused (wrong hash, empty-set hash, empty set, any hash on an empty set, missing id, other tenant's evidence, unsorted ids, unsorted ids over equal payload hashes, duplicate, NULL element, other tenant's row); five writers refused; update, delete and truncate refused for trust and for the administrator; tenant isolation and forced RLS; `evidence_verify` reads across tenants and fails on 25 other statements; exact catalog grants; the sealer stores row and Record event together and not when the Record append fails; empty set refused; recompute finds tampering, a gap, and fails closed on the wrong login; output keys and no leak.
- Mutation checks (migration): no hash check, no force RLS, extra SELECT on payloads, INSERT for the verifier, no immutability trigger, no SELECT policy, SQL hash without sort and without duplicate check, no ordering check (killed only by the equal-payload-hash case, added for it) all fail a test. Survivors, stated: the tenant filter on the items inside the guard, the `tenant_id` equality in the login lookup, the `power = 'trust'` test, and the empty-hash CHECK. Each is a second line behind row-level security, table grants or the cardinality CHECK, kept as defence in depth like `evidence_row_tenant_guard` in 0008; a test that reaches them alone would need a login that bypasses RLS or holds a grant it must not.
- Local runs: PostgreSQL 16.15 for the Go tests and `test_init.sh`; CI runs the script job on 17.11 (A38, D7).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Trust call site (TRU tasks) supplies the reconciliation and calls `Sealer.Seal`; OPS schedules `Recompute` and raises the alert on `AnyFailure` (the leak scanner noema#222 holds its output).
- Open questions: migration number (ledger, noema#183: 0019 is mine provisionally); EL owns the exception record for the new login; whether `evidence_ids` as an array (this PR) is accepted over an items table (Security Reviewer asked for no third table; IL to confirm).
- Files to read first: `services/core/migrations/0019_context_snapshot.sql`, `services/core/evidence/snapshot/README.md`.
- Do not touch: the golden vectors; `hash.go` (noema#225 owns its history).

# Session 2026-11-07 (later) · EVD-03 (slice 2) · Security Reviewer's finding on noema#268

## Session summary
- What changed: policy `evidence_item_definer_read` in migration 0019 (the definer guard reads `evidence_item` as its owner and the table forces row-level security); `init.sh` refuses an `evidence_verify` secret equal to another login's; `test_init.sh` seals one snapshot as the non-superuser migration login, refuses a wrong hash for the right reason, and checks the shared-secret refusal; the grants test also enumerates default privileges, reverse memberships, other schemas and database CREATE.
- Why: Security Reviewer, noema#268 at f841aa0: under a non-superuser owner the guard sees no evidence row and every snapshot is refused. Real, and invisible to the Go tests (the test migrator is a superuser). Reproduced first: `test_init.sh` printed "an evidence id does not name an evidence item of tenant ten_aa" for an honest snapshot.
- Tests added: `TestEverySecurityDefinerFunctionThatReadsAForcedTableHasAPolicyForItsOwner` (textual FROM/JOIN check, red before the policy), the `test_init.sh` sealing path (red before the policy), the shared-secret refusal, the extended grants enumeration. Red commit 3b94543.
- Debt entries: none. Conflicts raised: none.

# Session 2026-11-08 · EVD-03 (slice 2) · relay-issue answers

## Session summary
- Migration renumbered `0019_context_snapshot.sql` to `0020_context_snapshot.sql` (Repo Steward, relay issue #286: noema#265 holds 0019). Earlier sections of this file say 0019; that is the same file under its old number.
- EL's exception record for `evidence_verify` is the comment on relay issue #282; it is linked from noema#268. IL (issue #284) accepts `evidence_ids text[]` with no items table.
- `TestEvidenceVerifyHoldsExactlyTheGrantsD9Names` now covers every non-trigger function in `public`, not only SECURITY DEFINER ones (EL's condition 3, "EXECUTE on no function").

## Session 2026-11-11 11:30 UTC · EVD-03 slice 2, second-review items (C1's review of 10 Nov on noema#268)

## Session summary
- What changed: two tests that pin the Record-first order in `Seal` (`TestTheSnapshotIsNotInsertedBeforeTheRecordEventIsAppended`: after a failed append no snapshot row exists inside the open transaction; `TestAnInsertRefusedAfterASuccessfulAppendLeavesNeitherTheEventNorTheRow`: the append succeeds, the INSERT is refused, and after the rollback neither is left), and the stale "0019 is noema#265's" wording in the 0020 header and the migrations README (the ledger on noema#183 is 0017 #265, 0018 #261, 0019 #263, 0020 this file). The migration SQL is unchanged apart from that comment, which is allowed until the file is on `main`.
- Why: C1's items 1 and 2 on noema#268; A131 3c (a test must fail when the error path is ignored). The existing test rolled back before looking, so an INSERT placed before the append passed it.
- Risks, and how they are contained: the Postgres-backed tests could not run on this machine (per-tenant logins need passwordless local auth), so their first run is CI's, and the mutation (INSERT above the Append) is described, not run.
- Tests added: the two above (REQ-AUD-001, EC-E16).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after noema#263 lands, merge `main`; `init.sh`, `test_init.sh` and `testpg.go` conflict with #181 (C1's note) and #181's duplicate-secret list must cover `evidence_verify_password`.
- Do not touch: the sealer's order (Append, then INSERT).

## Session 2026-11-12 · EVD-03 slice 2 · A145 (no pull request edits `migrations/README.md`)

## Session summary
- What changed: the 0020 section is removed from `services/core/migrations/README.md` (the file is back to `main`'s) and its object table moved to `services/core/evidence/snapshot/README.md`. No code, SQL or test changed. A145 (committed in `DEC-2026-11-11-architect-rulings-a129-a147`, docs#156) ends the practice of each migration pull request adding a row there; the ledger lives on noema#183. Earlier sections of this file that mention "the migrations README" describe the state at their date.
- Why: A145. Also now committed and cited by this PR: A136 (row security with FORCE in the table's own migration) and A141 (merge in dependency order only, which supersedes A113's strict ascending order; A113's acceptance of the number gap stands). I find no parent of this PR in A141's list.
- Risks, and how they are contained: documentation only; the PR's code diff is unchanged.
- Tests added: none. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after Codex reads the delta, nothing in this PR; the Trust call site (TRU) calls `Seal`.
- Do not touch: `services/core/migrations/README.md`.

## Session 2026-11-13 · EVD-03 slice 2 · `main` merged; two test-world corrections (REC-08, and the owner the Compose migrator gives a function)
- **Why a push:** `core` at `5ae56643` (the merge of `main`, which carries EL's `0024` and so cleared the `evidence_verify` test) failed five tests of this package, all in my test code, not in the migration or the Go (https://github.com/Noemai-org/noema/actions/runs/43402277047/job/131807795642).
- **Four seal and recompute tests:** the test world built an unsigned appender ("no signing key for the emitter core-trust"). REC-08 reached `main` after this branch's tests were written. The world now holds `recordsig.All` signers and every appender in `seal_test.go` carries them, including the two that fail on purpose through `failingIDs`, which would otherwise have failed for the wrong reason (no signer) and not for theirs.
- **`TestEverySecurityDefinerFunctionThatReadsAForcedTableHasAPolicyForItsOwner`:** it flagged four Outcome functions (`outcome_record_verdict`, `outcome_verdict_complete`, `recovery_request_verdict_guard`) as owned by `postgres`. No migration gives them an explicit owner, so they belong to whoever ran the migrations: `noema_migrator` in Compose (covered by the `0018` policies, which name it), the superuser in this test cluster (which bypasses row security, so it neither needs nor has a policy). The query now treats an owner that is a superuser or has BYPASSRLS as standing for `noema_migrator`; an explicit owner that is any other role is checked as before. This does not weaken the check for any function that has an explicit non-superuser owner, and it adds no migration. If a reviewer would rather the four functions get an explicit `ALTER FUNCTION … OWNER TO noema_migrator`, that is a forward-only migration with a number from C1's table (noema#183); it would also first run the Outcome tests under the real non-superuser owner, which I cannot run here (no pgsodium).
- Debt: none. Conflicts raised: none.
