# Status · EXE-08 (Core Engineer 1) · W4, database-side verification of receipts

Updated 2026-11-05. Plan task EXE-08: owner C1; REQ-AUD-006, REQ-EXE-004; Needs EXE-07 (slice 1 merged in noema#129) and SEC-12; done when failing-first forgery, replay and downgrade tests pass and noema#94 and noema#90 can be released.

## State: slice A in review (draft until CI is green at the final head and Codex is READY). EXE-08 is not Done.

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

# Session 2026-11-07 10:00 WAT · EXE-08 (audit-record gaps, noema#243)

## Session summary
- What changed: three gaps in what the stored Record carries, each shown by a failing test first (commit on this branch, red `core` run linked in the PR): receipt `logical_action` is now the capability (reconstruction of a real write works); the swept receipt keeps the attempt time; `GRANT_VOIDED` and the revalidation events carry the grant's `policy_version`, served by the AUTHORITY projection. Audit table in `docs/security/record-provenance-audit.md`.
- Risks: the signed receipt document changes (`logical_action` value); no receipt exists outside tests, so no migration. Contract `execution-receipt.v1` already says PackId, so the contract is not changed.
- Tests added: REQ-AUD-003, REQ-EXE-006, INV-10, INV-11, EC-E09.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the new active policy version on a void needs Trust to write `POLICY_PUBLISHED` (TRU-08); WHO needs Edge events.

## Session 2026-11-06 (later) · ruling A24: the Ledger's rollback is bounded
- `execution/ledger` rolled back with `context.WithoutCancel(ctx)` and no bound, so a server that did not answer would hold the step forever. It now uses `context.WithTimeout(context.WithoutCancel(ctx), RollbackTimeout)` (`rollbackBounded`), and a Ledger with no `RollbackTimeout` refuses to start a transaction (no default; the value comes from configuration). Tests first: a hung rollback is abandoned after its bound and was tried although the caller's context was cancelled; an answered rollback returns at once; a Ledger without a bound refuses. Mutations caught: no timeout; the caller's cancellation inherited. The other rollbacks in `record/reconstruct`, `record/verify` and `platform/db` already carry a bound; `outcome/store` is noema#202.

## Session 2026-11-10 · EXE-08 / noema#243: one name for the dispatch capability (ruling A117)
- What changed: `execution/ledger/spend.go` no longer writes `capability_id` in `DISPATCH_ATTEMPTED`; the event keeps `capability` and `target_ref` (the execution-limits budget key). `record/reconstruct/project.go` reads `capability` and outputs it as `capability_id` in the reconstruction. No contract change, no debt entry (the reconstruction's output key is unchanged).
- Why: on 9 Nov I resolved a merge of `main` by writing both `capability` and `capability_id` and flagged it; the Architect ruled A117 on 10 Nov.
- Tests: failing first at `1b44c41` (`TestDispatchAttemptedNamesTheCapabilityOnlyAsCapability`; the two reconstruct dispatch tests with a payload that has only `capability`), green after.
