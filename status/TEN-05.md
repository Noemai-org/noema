# TEN-05 · the simulated clock advances with Temporal test time

Status: the Noema side is built (this task, noema TEN-05 PR). Tushe's part (time per object) is G0-T and is not here.

# Session 2026-11-17 · TEN-05 · the Temporal half (EL; A256 allows the Temporal test suite)

## Session summary
- What changed: `workflows/workflowstest` (test support, imported by no production code): `BindClock(env, sim)` makes the simulated clock (`clock.Manual`, SYNTHETIC) follow the Temporal test environment's time: each time a timer fires, the clock advances to `env.Now()` (never backwards). Tests: after three sleeps (36 h, 90 min, 24 h) an activity that reads the clock interface and the workflow's own `workflow.Now` see the same instants at every step, and the clock ends at the last; a control without the bridge shows the clock stays behind (so the test can fail); the bridge never moves a clock that is already ahead backwards; no production source file calls `clock.NewManual` ("only in tests and replays").
- Why: PERD §34.5 TEN-05 (the Noema-side condition of G0-N): the simulated clock applies only in tests and replays and advances together with Temporal test time. ENV-14's table showed this half as "Not shown".
- Evidence: red locally (the package had no non-test file), one push; three mutants killed (the bridge made a no-op, advancing by half, never advancing).
- Risks, and how they are contained: none in production; the helper is test support. The test environment's timer listener fires per timer; a workflow with no timers does not move the clock (nothing to follow).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: IL updates the TEN-05 row of ENV-14 to "Shown" with the CI link at the merged head; G0-N still needs TEN-07, TEN-08, `/partner`, the v1 tag and BO's fixtures (Founders and Tushe).
- Do not touch: `platform/clock`.
