
# Session 2026-11-04 17:10 WAT · OUT-01

## Session summary
- What changed: merged `main` (#145: tenant-bound Record functions, migrations 0003 to 0006); Outcome's test worlds now use `testpg.TenantLogin(..., "outcome_rw", tenant)`, and the e2e stack uses tenant logins for execution, trust and outcome. No production code changed: `Store` already appends through `chain.Appender` on the pool's login, which `record_insert_t` maps to the tenant with `session_user`. Migration `0007` is the only Outcome migration and does not collide.
- Tests: `go test -race ./...` all packages ok, except `platform/db` `TestAMigrationLockIsNeverLeftOnAPooledConnectionWhenTheUnlockFails`, which failed once in the full parallel run and passed 3 of 3 alone; it is not touched by this PR (suspected shared advisory-lock contention across packages; not investigated).
- Debt: unchanged.

## Pre-work for the next agent
- Exact next step: Compose check at the new head; then EVD-01 slice 2 (migration number after 0007).

## Addendum 2026-11-05 · answering the reviewer's NOT READY at `ed126b8`
- `Store` needed no code change: `chain.Appender.Append` (main, #145) calls `record_insert_t`, which finds the tenant and the emitter from `session_user`. The wiring is the pool's login; the test worlds now use `testpg.TenantLogin(..., "outcome_rw", tenant)` (`ed126b8`).
- Added `outcome/store/boundary_test.go` (INV-11, INV-15, EC-E16, DEBT-04): Outcome's Record events are written on the tenant's login with `written_by = core-outcome` and the chain verifies; an `outcome_rw` login not mapped to a tenant stores no verdict, no Record event and no outbox row. Both passed when written (guards, not seen failing); no mutation check run on them.

# Session 2026-11-06 · OUT-01 follow-up (bounded rollback)

## Session summary
- What changed: the Outcome store's rollback after a failed write no longer runs on a context with no deadline (the reviewer's P1 on noema#172 found the same pattern in the Evidence builder; this is my own merged code). `outcome/store/store.go`: `rollback(ctx, tx, limit)` detaches from the caller's cancellation but is bounded; the rollback's own error is joined to the original one, not swallowed. `Store.RollbackTimeout` is a new field and a store without a positive one refuses to write: the limit is a safety parameter the deployment owner sets (REQ-AGT-005), the arch-lint's threshold-literal rule forbids a literal in this package, and no config mechanism exists yet (`platform/config` is empty), so no default is chosen here.
- Tests: `TestEveryDetachedContextInTheStoreHasItsOwnDeadline` (a source guard, red first) and `TestAStoreWithoutARollbackTimeoutRefusesToWrite` (mutant caught). Test worlds set 5 s.
- Debt: `cmd/core` must supply `RollbackTimeout` from configuration with the Outcome wiring (DEBT-10). Same pattern remains in `execution/ledger/ledger.go` (Execution's owner).
