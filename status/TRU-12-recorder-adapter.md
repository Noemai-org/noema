# Status · TRU-12 · the intake.Recorder adapter (T15)

Updated 2026-11-19. State: in review (draft PR). Owner: lane agent for EL, STM-01 composition follow-ups. Rulings A329, A330, A332, A340 are relayed, not yet committed.

## Session 2026-11-19
- What changed: `services/core/orchestrator/portadapters/trust/recorder.go` and `recorder_test.go` (`RecorderAdapter`, `NewRecorderAdapter`, `Record`); no other file.
- Why: the intake's T15 needs a `Recorder` that writes the Decision Record, the grants and their events together or not at all (PERD 32.4, EC-E16).
- Choices: the adapter opens and commits the transaction through a `TxBeginner` the composition passes (one tenant login per tenant). Checks that need no database (basis digest, confirmation plan hash, facts shape) run before `Begin`. A context that ends while `Record` runs: rollback and the context's error, nothing committed. A failed commit returns `ErrCommit`: the commit may have reached the database, so a retry returns the stored ALLOW (A186).
- Failing first: tests-only commit with a stub that compiles and fails on behaviour.
- Mutations by hand, each red: 16 effective mutants killed (digest compare, plan-hash compare, the pairing of event and plan hash, context check, rollback after a writer error, rollback context not cancellable, grant count, facts count (two directions), principal, policy, builder passed on, tenant passed to Begin, commit error wrapping, plan step, per-step target versions, rollback after a failed commit). Five first survived and were killed by strengthening the tests.
- Not run here: the three tests that write a decision to a real database (`TestRecordWritesTheDecisionTheGrantsAndTheirEventsTogether`, `TestASecondRecordOfTheSameDecisionReturnsTheStoredOne`, `TestRecordOfAReadWritesNoGrant`) need pgsodium, which this sandbox lacks: they run in CI. The fake-writer tests and the kill-switch and failed-grant tests ran here.
- Safer readings taken: see the PR body.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the composition builds a `TxBeginner` (tenant pools), a `RecordFactSource` (policy ID, evidence set hash, target versions and freeze permission per write step; the T9 to T11 stores) and `grants.Keys` from `keyfiles.Load` (NOEMA_GRANT_KEY_DIR), then calls `NewRecorderAdapter`.
- Open questions (EL): where the evidence set hash and target versions come from; whether the intake's 0-based `PlanStep` (`int64(i)`) is what `grants` accepts.
- Files to read first: `services/core/trust/recorder/recorder.go`, `services/core/trust/intake/ports_t13_t15.go`.
- Do not touch: `trust/**`, `port.go`, `dispatches.go`.
