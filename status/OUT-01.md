
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

# Session 2026-11-07 (later) · OUT-01 / TST-05 · the third S9 kill point

## Session summary
- What changed: `outcome/verifier/boundary.go` (`Point`, `PointAfterVerdictStored`, `Points()`, `ErrCrash`), `Config.Boundary`, one call in `verdict.go` after `RecordVerdict` succeeds, `verifier_boundary_test.go`. PR: noema#320. Answers relay issue noema#318 (IL needs C2).
- Why: rulings A30 and A40: the shared kill-point list has a third S9 point, "after verify, before the note", that no boundary hook reached.
- Tests added (REQ-TST-004, REQ-VER-001, INV-04): the hook runs once and only after the verdict is stored; a crash there returns the crash and no result, keeps the verdict, and a redelivery finds it and stores nothing new; a replay crosses no boundary; any hook error is returned; the point list and name. Red first (does not compile). Mutants killed: hook call removed (3 failures), hook moved before the store (2 failures).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: IL wires `verifier.Points()` into the shared kill-point list of noema#252 and kills the core at `PointAfterVerdictStored`.
- Do not touch: the Store contract.
