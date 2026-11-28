# Status · INP-01 slice 6a · the ended state of an investigation (Core Engineer 2), STACKED ON noema#1075 (slice 2, on noema#1053)

Updated 2026-11-28. All data is SYNTHETIC. PR noema#1077.

## Built
`orchestrator/portadapters/resumestate.States` implements `understanding/resume.States` over the investigation row: the chain `corr_<ULID>` names the investigation `inv_<ULID>` (A234); the row is read through `investigations.Store.Read` as the session's tenant and principal; the investigation has ended when its state has **no exit in the orchestrator's transition table** (`orchestrator.LoadTable`), so the table, not a list here, decides. A chain that is not `corr_<ULID>`, a session with no tenant or principal, a missing row, a state the table does not know, a row of another principal (an old row holds none and is read by tenant alone) and a store that cannot answer are each `ErrRead`, never "open". The port of #1075 now carries the session's tenant and principal (a commit on that branch).
- **Red first:** `1706c7bd2`. **Hand mutants:** 9 killed.

## Not done
| Item | Owner |
|---|---|
| `Denials` over the Record: `RecordRecorder.Denied` (SECURITY_EVENT `CROSS_SCOPE_ATTEMPT`), a commit on #1076 | C2 |
| the edge's call (`cmd/core/conversations.go`), composing the resolver, the Interpreter's `Resumed` and `ContradictionFlag`; PUS-08 end to end, US-11 and the concurrent follow-up on real Postgres (CI only) | EL composes, C2 writes the tests |
