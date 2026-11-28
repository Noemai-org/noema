# Session 2026-11-28 · EXE-02 · A776 (C1, interim, relayed): the ledger deadline test runs on a fake clock

## Session summary
- What changed: `TestTheContextDeadlineStopsTheAttempts` (wall-clock) is replaced by `TestTheContextDeadlineOnAFakeClockStopsTheAttempts` in `execution/ledger/retry_deadline_test.go`. The deadline is an instant of `platform/clock.Manual`, passed to `Spend` as a context (`clockDeadline`); the test advances the clock when the client issues its third INSERT (a hook on the statement counter), so the order of events does not depend on machine speed. No production code changed.
- Why: the old test used a 500 ms `context.WithTimeout`, a 50 ms server sleep per attempt, a 300 ms real settle wait and a 10 s real guard. On a loaded runner the deadline could end before the first INSERT was issued (`insertsAtReturn < 1`) or the settle wait could miss a late statement. It passed 15 of 15 and 25 of 25 under CPU load here, so the flake is not reproducible locally; the cause is read from the test (four real-time dependences), and the replacement has none.
- The same properties are asserted: an error (the did-not-commit error or the context's), no spend, a bounded number of attempts (the three that ran up to the deadline, at most one more), and no statement of the client after the return. The settle sleep is replaced by `pool.Close()`, which returns when every connection is released.
- Tests added (REQ-EXE-001, REQ-EXE-002; INV-03, INV-07, AGENT-08). Regression case: the red at `e18f22986` fails with 1000 attempts reached their INSERT. 50 consecutive runs pass, 20 of them under CPU load. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none.
- Do not touch: `execution/ledger` production code (no threshold or retry budget changed).
