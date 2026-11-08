# TST-04 · Test-suite audit

Date: 2026-11-07 · Task: TST-04 (REQ-TST-001, REQ-TST-004, REQ-TST-006) · Audited at `main` 347263f · Author: Engineering Lead role agent · Status: audit note; not acceptance (an agent's report is not acceptance, plan §4.1).

This note describes what was found. It defines no requirement. Three questions, from the task row: (1) no mocks in integration or benchmark packages; (2) no benchmark-only branches; (3) every invariant and REQ test written before its code, judged from PR history.

## 1. No mocks in integration or benchmark packages (REQ-TST-004, AGENT-12)

- **Enforced, not only observed.** `tools/lint/golint/manifests.go` (`mockRule`) refuses the Go mock libraries listed in `goMockLibraries` in benchmark and integration packages; `tools/lint/pyrules.py` (`mocks`) refuses `unittest.mock`, `mock`, `pytest_mock`, `responses`, `httpretty`, `requests_mock`, `respx` and `moto` there. Each rule has a canary under `tools/lint/canaries/` (for example `mock-library/benchmark/h.go`).
- **Search at 347263f** (all `*.go`, `*.py`, `*.ts`, `*.tsx`, `*.js`, excluding vendored environments): no import of a mocking library in any tracked test. The one `monkeypatch` use (`services/model/tests/test_main_telemetry.py:37`) sets a process environment variable for the default app; it replaces no collaborator.
- **Test doubles that are not mock libraries, listed for the reviewer's decision (not changed here; each is outside integration and benchmark packages):**

| # | Where | What it is | Why it is listed |
|---|---|---|---|
| 1 | `services/core/platform/egress/egress_test.go` (`fakeResolver`) | A hand-written DNS resolver returning fixed answers to the callback egress guard | A resolver answer is a fault (DNS rebinding, NXDOMAIN) produced by the test, not by a Tushe FaultPlan or the hostility proxy (AGENT-12). It is a unit test of one interface, but the rule says faults come from FaultPlans or the proxy. **Decided by the Security Reviewer (noema#293, 7 Nov 2026): stays as a recorded exception, named "resolver seam in `platform/egress` only".** Reason: the property under test (DNS rebinding: one name's answer changes between registration, delivery and dial) cannot be driven by the hostility proxy, which sits on HTTP routes and not on DNS; the real network is still used where it matters (`TestTheTransportDialsTheCheckedAddressAndNothingElse` dials a real listener). Condition: when callback delivery is wired (SEC-05 slice 2) one test with the system resolver is added (a name that resolves to loopback is refused at registration and at delivery, no fake); SEC-05 stays In progress until it exists. |
| 2 | `services/core/trust/decide/proposer_swap_test.go` | A canned-proposal stub (Proposer A) and recorded provider replies (Proposer B) | Recorded replies are labelled and the real `EscalateCase` runs on mock-case; the stub is a test input, not a mocked failure mode. Listed because it is the largest double. |
| 3 | `services/core/adapters/synthetic_tushe/case_client_test.go` | Exchanges recorded from the real mock-case, labelled SYNTHETIC | A recording, not a mock; listed so the label check (AGENT-25) is visible. |

The end-to-end packages (`adapters/outcome_e2e`, `adapters/synthetic_tushe` fault tests) use the real mock-case, the real hostility proxy and real Postgres (`testpg`), per the stack table in AGENTS.md §6.

## 2. No benchmark-only branches (REQ-TST-006)

- Searched the production code of `services/core` and `services/model` for benchmark, oracle, held-out and profile switches. `services/core` has none: its only environment reads are `NOEMA_CORE_ADDR` (`cmd/core/main.go:81`) and the test harness's database URL (`internal/testpg/testpg.go:244`). Comments that mention the benchmark describe who reads a field, not a code path.
- **`BENCH_OUT` in `services/model/noema_model/main.py` and `platform/telemetry/__init__.py` (REQ-NFR-007): accepted by the Architect on 8 Nov 2026 as an observability-only exception for the POC (noema#293).** When set (only by the bench override of the Compose stack), each model call's record is also written as a cost file; it changes no request, response, decision or provider choice. The Architect's two conditions are tests in `services/model/tests/test_cost_files.py`, both passing: `test_decisions_are_identical_with_and_without_bench_out` (the returned call and everything the wrapped telemetry sees are identical with it set and unset) and `test_cost_files_are_written_only_under_bench_out_cost_and_never_through_a_link` plus `test_the_bench_compose_override_mounts_benchmark_output_and_no_held_out_custody` (INV-22). **The custody test found a real gap, now fixed:** the writer followed a symbolic link at `$BENCH_OUT/cost`, so the files could have been carried anywhere, including into held-out custody. `CostFileTelemetry` now refuses a linked or outside cost directory (`CostFileRefused`).
- `benchmark/` code is outside this question (it is the benchmark).

## 3. Tests before code, from PR history (AGENT-11, REQ-TST-001)

**Limit of this evidence.** Git order shows which commit introduced a test and which introduced the code. It cannot show that the test was *seen failing in CI*; that proof is the CI run history of the PR, which this audit did not reproduce. The result below is therefore a floor on discipline, not proof of it.

Method: for each of the 109 merged PRs on `main`'s first-parent history that changed source code, compare the first commit that touches a test or fixture with the first that touches source code (script not committed; the rule is reproducible by `git rev-list --reverse <parent1>..<parent2>` per merge commit).

| Result | PRs |
|---|---|
| A test or fixture commit before the first source commit, or in the same commit as it | 100 of 109 |
| Source before any test | 9 (a second pass restricted to P0 paths flags 7 more; 14 distinct PRs in all, every one read below) |

Restricted to the P0 paths (`services/core/{trust,execution,outcome,record}`, `packs/`, `contracts/schemas/`; 78 PRs): 34 have a test commit strictly before the code, 37 have test and code in one commit (order cannot be shown), 7 are listed as exceptions.

**The 14 PRs flagged by either pass, read individually:**

| PR | Finding |
|---|---|
| #41, #37, #54, #35 (P0 pass or all-paths pass) | False positives of the script: the first commit is a fixture or inventory commit marked "before the schema (seen failing)", then the schema, then the tests and code. The order is test-first. |
| #15, #7 | Contract restoration PRs (CON-01): fixtures and schemas land together or via merged sub-branches; no logic. Order not provable from history. |
| #52 | One commit adds fixtures and the schema reason together (PROVIDER_FAILOVER reason). Same-commit. |
| #204, #114 | Compose profile checks: the check script is itself the test. No separate failing-first commit. |
| #139 | The first commit is "failing test first" (`assert_roles.sql`); a false positive because the assert script is `.sql`. |
| #111, #58, #22, #16 | A spike, a matrix-row addition, a docs re-point and a CI/compose governance PR. Not product logic. |

**No PR in Trust, Execution, Outcome or Record was found with source strictly before its tests.** 37 same-commit P0 PRs cannot be proved test-first from history; for those the PR's CI run is the only evidence, and the reviewer should not read this note as proof.

## 4. What is not done

- The CI run history for the same-commit P0 PRs was not examined.
- Both exceptions are decided and recorded (the resolver fake by SR, `BENCH_OUT` by the Architect), so the task's "any exceptions fixed" is met: one stays as a recorded exception with a follow-up test owed by SEC-05 slice 2, the other is accepted with two tests and one fix. The 37 same-commit P0 PRs are not proved test-first by history; their CI runs were not examined.
- Python test packages other than `services/model/tests` were searched by import only.

## 5. Suggested follow-ups (for the Steward to place; none is started)

1. A CI check that a PR whose title cites a task in a P0 path has at least one commit that adds a test before the first source commit (would make section 3 a check, not an audit). It belongs to MEM-02 or PLT-09, not here.
2. SEC-05 slice 2 owes the system-resolver test (a name that resolves to loopback is refused at registration and at delivery, no fake).
