# Status · TST-05 (Core Engineer 1) · the mock-case harnesses hold their own listener (noema#525, noema#526)

Updated 2026-11-13.

# Session 2026-11-13 17:30 WAT · TST-05

## Session summary
- What changed (noema#526): `adapters/outcome_e2e/mockcase_test.go` and `execution/e2e/mockcase_test.go` no longer pick a port. The mock-case process is started on `127.0.0.1:0` (case and bench listeners), holds its own listeners from its first instant, and the harness reads the addresses it logs (`mock-case listening (SYNTHETIC)`). New: `no_probe_test.go` (the guard), `mockcase_listen_test.go` in each package (the log-line parser and four mock-cases started at once). No change to `synthetic/mock_case`.
- Why: the Benchmark Owner's report on noema#525: `TestEveryCrashPointOfTheExecutionProtocolLeavesARecordThatReconstructs` failed once with `mock-case stopped: listen tcp 127.0.0.1:37261: bind: address already in use`. The old `freePort` listened on port 0, read the port, closed the listener and told the process to bind that number; any other process that bound an ephemeral port in the gap won it.
- Reproduction (a standalone program, in the PR description): 64 starters at once, probe then bind 2 ms later, 300 rounds: 77 of 19,200 and 71 of 19,200 binds failed (about 0.4 percent). The real test is much rarer: 24 runs of the real test (12 processes at once, twice) failed 0 times on both the old and the new harness. I therefore do **not** claim a measured drop in the real failure rate; the claim is that the mechanism is gone by construction (no port exists before it is bound) and that a guard stops it returning.
- Risks, and how they are contained: test code only. A process that dies before it reports its addresses fails the test at once with its log; an address that is not 127.0.0.1 with a bound port is refused; each readiness probe has a 5 s timeout.
- Tests added (REQ-TST-001, REQ-TST-008): `TestNoMockCaseHarnessChoosesAPortBeforeItIsBound` (red first, run `338e70e4`: the `core` job failed on exactly this), `TestParseListening*` (three), `TestMockCasesStartedAtOnceEachHoldTheirOwnListeners` (four processes alive at once, eight distinct addresses, each held by its process).
- Regression cases added: the guard.
- **Debt, named, not fixed here:** `internal/testpg/testpg.go` has the same shape for PostgreSQL (`freePort`, then `pg_ctl -p PORT`); PostgreSQL cannot bind port 0, so the fix there is a bounded retry with a new port when `pg_ctl start` reports the port taken. Text for the Steward's debt register: "testpg starts the throwaway PostgreSQL on a probed port (probe-then-bind race, about one start in a few hundred thousand); owner C1; closes with a bounded retry in `testpg.start`". `benchmark/oracle_client/live_check.py` has the same shape in Python (not mine).
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after merge, close noema#525 (the Benchmark Owner's re-run request on noema#199 is moot once its job passes) and decide on the `testpg` retry.
- Files to read first: `adapters/outcome_e2e/mockcase_test.go` (`parseListening`, `boundAddrs`), `no_probe_test.go`.
- Do not touch: `synthetic/mock_case` (Integration Lead's).

## Session 2026-11-14 · TST-05 · EL's P2 on noema#526
- What changed: `TestMockCasesStartedAtOnceEachHoldTheirOwnListeners` (both packages) no longer assumes four free parallel slots. Its group is `min(4, -parallel)`; a limit of one fails loudly (`t.Fatalf`, not a skip) because the property needs two processes at once. The old shape hung until the test timeout with fewer than four slots (reproduced: `-parallel 1` on the old test ran 5 minutes without finishing). Checked: `-parallel 1` fails at once with the message, `-parallel 2` and `-parallel 4` pass in both packages. The two notes of the reviewers are in the comments (the guard sees only a literal `:0`; the log line is a contract with `cmd/mock-case`).
- Pre-work for the next agent: the harness is about to move to an importable package (`internal/e2estack`, this session's next PR); keep `parallelSlots` with the test that needs it.
