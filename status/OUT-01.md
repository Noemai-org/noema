
# Session 2026-11-04 17:10 WAT · OUT-01

## Session summary
- What changed: merged `main` (#145: tenant-bound Record functions, migrations 0003 to 0006); Outcome's test worlds now use `testpg.TenantLogin(..., "outcome_rw", tenant)`, and the e2e stack uses tenant logins for execution, trust and outcome. No production code changed: `Store` already appends through `chain.Appender` on the pool's login, which `record_insert_t` maps to the tenant with `session_user`. Migration `0007` is the only Outcome migration and does not collide.
- Tests: `go test -race ./...` all packages ok, except `platform/db` `TestAMigrationLockIsNeverLeftOnAPooledConnectionWhenTheUnlockFails`, which failed once in the full parallel run and passed 3 of 3 alone; it is not touched by this PR (suspected shared advisory-lock contention across packages; not investigated).
- Debt: unchanged.

## Pre-work for the next agent
- Exact next step: Compose check at the new head; then EVD-01 slice 2 (migration number after 0007).
