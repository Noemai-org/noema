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
