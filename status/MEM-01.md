# Status · MEM-01 (Core Engineer 1) · operational memory

Updated 2026-11-07. Plan task MEM-01: owner C1; REQ-MEM-001; needs REC-01; done when "lint and test prove Trust never reads memory tables; the EC-X15 poisoning case changes no permission, policy or action".

## State: step 2a in review (draft). MEM-01 is not Done.

## Session 2026-11-07 · MEM-01 (Founder decision D10)
### Session summary
- What changed (PR links): step 1, `memory-observation.v2` with `provenance_ref` (noema#266); step 2a, migration `0020_memory_observation.sql` and `platform/db/memory_observation_test.go` (this PR, stacked on #266): the table is closed to every runtime role, the only write path is `memory_observation_record` for `orchestrator_rw`, tenant from the login, append-only, typed scalars only.
- Why: D10 (writer `orchestrator_rw` only, observation fields typed scalars) and REQ-MEM-001 (nothing in Trust, Execution, Outcome or Understanding reads memory at decision time): the database makes that true for the four powers regardless of imports.
- Risks, and how they are contained: a new writer path; contained by the role check inside the function (a member of any other power is refused even when it also holds orchestrator_rw) and by granting EXECUTE to `orchestrator_rw` only. No read path exists.
- Tests added (REQ-MEM-001, INV-12, REQ-SEC-004): `TestOnlyTheOrchestratorLoginRecordsAnObservationAndItLandsInItsOwnTenant`, `TestMemoryObservationsAreAppendOnlyAndHoldTypedScalarsOnly` (committed red first at 793f142).
- Regression cases added: none. Debt entries: none. Conflicts raised: none. The per-kind field lists are still open (their owners); `observation` stays typed scalars.

## Pre-work for the next agent
- Exact next step (step 2b): `record/memory` (Go): `Store.Record(ctx, tx, Observation)` calls the function and appends `MEMORY_OBSERVATION_RECORDED` through `chain.Appender` in the same transaction (emitter `core-workflows`; the event commits with the row or neither does); validate against the generated `contracts.MemoryObservationV2` and refuse free text before the database does; the import-boundary lint rule and canary for "no decision-path package imports `record/memory`" (`tools/lint/golint`); and the EC-X15 end-to-end case belongs to the benchmark owner.
- Open questions (owner): the migration number (Steward, noema#183); the read role for the ops inspector (Architect).
- Files to read first: `services/core/migrations/0020_memory_observation.sql`, `services/core/record/memory/doc.go`.
- Do not touch: `memory-observation.v1.json` (superseded, kept).
