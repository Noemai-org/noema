# Status · MEM-01 (Core Engineer 1) · operational memory

Updated 2026-11-07. Plan task MEM-01: owner C1; REQ-MEM-001; needs REC-01; done when "lint and test prove Trust never reads memory tables; the EC-X15 poisoning case changes no permission, policy or action".

## State: step 2a in review (draft). MEM-01 is not Done.

## Session 2026-11-07 · MEM-01 (Founder decision D10)
### Session summary
- What changed (PR links): step 1, `memory-observation.v2` with `provenance_ref` (noema#266); step 2a, migration `0021_memory_observation.sql` and `platform/db/memory_observation_test.go` (this PR, stacked on #266): the table is closed to every runtime role, the only write path is `memory_observation_record` for `orchestrator_rw`, tenant from the login, append-only, typed scalars only.
- Why: D10 (writer `orchestrator_rw` only, observation fields typed scalars) and REQ-MEM-001 (nothing in Trust, Execution, Outcome or Understanding reads memory at decision time): the database makes that true for the four powers regardless of imports.
- Risks, and how they are contained: a new writer path; contained by the role check inside the function (a member of any other power is refused even when it also holds orchestrator_rw) and by granting EXECUTE to `orchestrator_rw` only. No read path exists.
- Tests added (REQ-MEM-001, INV-12, REQ-SEC-004): `TestOnlyTheOrchestratorLoginRecordsAnObservationAndItLandsInItsOwnTenant`, `TestMemoryObservationsAreAppendOnlyAndHoldTypedScalarsOnly` (committed red first at 793f142).
- Regression cases added: none. Debt entries: none. Conflicts raised: none. The per-kind field lists are still open (their owners); `observation` stays typed scalars.

## Pre-work for the next agent
- Exact next step (step 2b): `record/memory` (Go): `Store.Record(ctx, tx, Observation)` calls the function and appends `MEMORY_OBSERVATION_RECORDED` through `chain.Appender` in the same transaction (emitter `core-workflows`; the event commits with the row or neither does); validate against the generated `contracts.MemoryObservationV2` and refuse free text before the database does; the import-boundary lint rule and canary for "no decision-path package imports `record/memory`" (`tools/lint/golint`); and the EC-X15 end-to-end case belongs to the benchmark owner.
- Open questions (owner): the migration number (Steward, noema#183); the read role for the ops inspector (Architect).
- Files to read first: `services/core/migrations/0021_memory_observation.sql`, `services/core/record/memory/doc.go`.
- Do not touch: `memory-observation.v1.json` (superseded, kept).

## Session 2026-11-11 · MEM-01 step 2a: row-level security with FORCE (A6, REQ-SEC-004) and the header ledger numbers
- What changed (PR links): noema#272: `0021_memory_observation.sql` enables and FORCES row security on `memory_observation` with one INSERT policy for the owner whose check is `record_login_tenant('write')` (no policy for SELECT, UPDATE, DELETE or ALL); the header and the README name the ledger numbers correctly (0017 is #265, 0019 #263, 0020 #268, 0021 this PR; strict ascending order, A113; the gap is accepted).
- Why: the Architect's directive of 10 Nov (ruling A6 and REQ-SEC-004, which are committed: a tenant table has RLS plus FORCE; the Architect's A136 states the same rule and is recorded in docs#156, still open, so nothing here rests on it); Codex and C2 found the numbers stale.
- Tests: failing first at f66d0346 ([run](https://github.com/Noemai-org/noema/actions/runs/42972198650/job/130363014814), `core` red): `TestMemoryObservationHasForcedRowSecurityWithOnlyAnOwnTenantInsertPolicy` and `TestTheOwnerCannotInsertAnObservationOutsideTheWritePath` (the owner, with no tenant login behind it, inserts directly before the change). The existing orchestrator-login tests prove the write path still works under the forced policy, because the test cluster's `noema_migrator` is not a superuser.
- Debt entries: none. Conflicts raised: none.
