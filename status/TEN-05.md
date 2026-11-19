# Status · TEN-05 the simulated clock advances with Temporal test time (G0-N)

Updated 2026-11-17. Owner: C1 (the Noema half); IL prints and records the TEN_RESULT line.

# Session 2026-11-17 · TEN-05 (A256, interim)

## Session summary
- What changed: `services/core/workflows/ten05_test.go`. A workflow sleeps 48 hours in the Temporal test environment (`go.temporal.io/sdk/testsuite`, A256: the "Temporal test environment" of PERD 36.1, so no [OPEN] entry); the simulated Noema clock (`clock.Manual`, SYNTHETIC) follows the environment's time when a timer fires; an activity reads it through the `clock.Clock` interface. Before the sleep it reads the start, after it start plus 48 hours, equal to the environment's time; no real time passes (the test fails above 10 s). A second test: no production file outside `platform/clock` builds or names `clock.Manual`, so the simulated clock exists only in tests and replays.
- The line: `TEN_RESULT id=TEN-05 seed=none clock=SYNTHETIC skipped=48h0m0s pass=true` (a `t.Logf` of the first test; run it verbose, `go test ./workflows/ -run TEN05 -v`). The CI grep that records it in the G0-N evidence is a workflow change, which is IL's (AGENTS.md section 11), so none is added here.
- Not claimed: the first half of TEN-05, "Tushe time is controlled per object" (the target's own clock endpoint), which the Tushe acceptance worksheet and mock-case tests cover.
- Tests added (REQ-ENV-005, INV-21): above; mutant: without the listener the clock stays at the start and the test fails ("read ... 09:00, want ... 19 Nov 09:00"). No red CI run: the behaviour already holds.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: IL adds the `TEN_RESULT id=TEN-05` grep to the core workflow.
