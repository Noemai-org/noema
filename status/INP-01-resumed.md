# Status · INP-01 slice 4 · INVESTIGATION_RESUMED (Core Engineer 2), STACKED ON noema#1074 (slice 3, which stacks on noema#1053)

Updated 2026-11-28. All data is SYNTHETIC. PR noema#1076.

## Built
- `interpreter.Resumes` (optional `Config.Resumed`): after a follow-up (an `Input` with a `Prior`) has passed the gate and before anything is saved, the Interpreter records `INVESTIGATION_RESUMED`; a Record that cannot take it stops the save (`ErrRecord`, EC-E16). A first turn, a follow-up the gate refuses, and an Interpreter with no port record no resume.
- `RecordRecorder.Resumed`: appends the event as the orchestrator's emitter (`core-workflows`) for the login's tenant: `object_ref` is the prior's intent id, the payload is `prior_intent_id` and `prior_version` only (AGENT-13); an id the store could not hold or a version below one is refused and nothing is appended.
- **Red first:** `8bd0e069c` (the port type and a stub; the DB tests run only in CI because `pgsodium` is not available to a local run: the local red of those two is the stub's, recorded in the PR comment). **Hand mutants:** 4 killed (never recorded; version off by one; failure swallowed; a first turn records a resume).

## Not done
| Item | Owner |
|---|---|
| passing the `RecordRecorder` as `Config.Resumed` in `cmd/core` (and as the resolver's `Denials`) | EL |
| slice 6: the `States` and `Denials` over the Record, the edge call, PUS-08 end to end, US-11 and the concurrent follow-up on real Postgres | C2 with EL's read |

## Added later the same day (slice 6b): the denial event
`RecordRecorder.Denied(ctx, tenantID, actor, correlationID)` appends `SECURITY_EVENT` of kind `CROSS_SCOPE_ATTEMPT` for a follow-up that named another principal's investigation (`understanding/resume.Denials`, #1075): the session's tenant and the asking actor, the chain asked for as the subject, and the payload `{"scope":"PRINCIPAL"}` only (no principal or reference of the other customer, AGENT-13). A missing tenant, actor or chain is refused and nothing is appended. Red `241f4910a` (stub and two database tests, CI only); green after it.
