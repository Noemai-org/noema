# adapters/outcome_e2e

**What it does.** End-to-end tests of Outcome's verify-by-key (OUT-01): a create is dispatched through the real Execution controller to the real `mock-case`, a fault from a committed FaultPlan is applied, and Outcome's verdict is checked against what the target really holds (its truth endpoint) and against the Record. No production code.

**REQs.** REQ-VER-001, REQ-VER-003, REQ-RCV-002; INV-04, INV-07; US-06, US-16, EC-E03.

**Tests.** `go test ./adapters/outcome_e2e/...` from `services/core/`.
- US-06: `mock_case_commit_then_drop.json` as committed. The target commits and drops the response; the receipt is `OUTCOME_UNKNOWN`; Outcome returns ACHIEVED with one dispatch, one case and no second create.
- US-16: the write never commits (`TIMEOUT_BEFORE_COMMIT`); NOT_ACHIEVED with one RecoveryRequest.
- EC-E03: a 200 with a semantic PENDING creates nothing; never ACHIEVED.
- A 5xx on Outcome's own first read is retried after 1 s of simulated time; injected text in the target's answer changes nothing.

**The test-only control client** (`bench_test.go`, DEC-2026-10-26 Amendment 1) loads the FaultPlan and reads the truth endpoint. `boundary_test.go` fails if any non-test file under `services/` names a control or truth path or the bench credential, or imports a package of this repository outside `services/core`.

**Why here.** `import-boundary` and `import-adapters` apply to test files: only `execution`, `evidence` and `adapters` may import the case adapter, and `outcome` may not reach `execution`. The OUT-01 card named `outcome/e2e`; no test there can dispatch, read and verify. Recorded as a deviation in the PR.

**Duplicated on purpose.** `mockcase_test.go` is a copy of the harness of `execution/e2e` with the bench listener kept. A shared test helper is a refactor of its own.

## The crash matrix (TST-05, ruling A30: option B)

`crash_test.go` (Linux only) kills the core with SIGKILL at a named boundary of the controller, in a separate process, so nothing is cleaned up and PostgreSQL rolls the open transaction back. The test re-executes its own binary (`TestCrashChild`); the child attaches to the parent's database through the tenant's own logins, wires the real controller, ledger and case client around the real `mock-case`, and kills itself at the point. The parent then reads the database and the target's own command log (its truth), runs the recovery path, and checks the effect count at every step: never more than one. It runs in the existing `core` job (`go test -race ./...` against PostgreSQL 17.11); no workflow, lint rule or non-test code was added.

**The kill-point list, shared with RESIST S9 (ruling A40).** One list, so the matrix and the benchmark family name the same points:

| Point | Controller boundary | S9 family point | Status |
|---|---|---|---|
| before the spend commits | `PointInSpendBeforeCommit` | S9.1 before the write | built (slice 1) |
| after the spend commits, before dispatch | `PointAfterSpend` | S9.1 before the write | built (slice 1) |
| after dispatch, before the response is classified | `PointAfterDispatch` (and `TIMEOUT_BEFORE_COMMIT` from a FaultPlan) | S9.2 after the write, before verify | slice 2 |
| the target commits and the response is lost | FaultPlan `TIMEOUT_AFTER_COMMIT` or `DROP_RESPONSE`, killed when the truth endpoint shows the command committed | S9.2 after the write, before verify | slice 2 |
| after verify, before the note | no hook yet (Outcome and the explanation have no boundary) | S9.3 after verify, before the note | needs a hook from Core Engineer 2 |

The other nine controller boundaries are not in this list yet; the simulated matrix in `execution/controller` covers all fourteen against a simulated target.
