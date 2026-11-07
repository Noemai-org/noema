# TST-04 · Test-suite audit

Date: 2026-11-07 · Task: TST-04 (REQ-TST-001, REQ-TST-004, REQ-TST-006) · Audited at `main` 347263f · Author: Engineering Lead role agent · Status: audit note; not acceptance (an agent's report is not acceptance, plan §4.1).

This note describes what was found. It defines no requirement. Three questions, from the task row: (1) no mocks in integration or benchmark packages; (2) no benchmark-only branches; (3) every invariant and REQ test written before its code, judged from PR history.

## 1. No mocks in integration or benchmark packages (REQ-TST-004, AGENT-12)

- **Enforced, not only observed.** `tools/lint/golint/manifests.go` (`mockRule`) refuses the Go mock libraries listed in `goMockLibraries` in benchmark and integration packages; `tools/lint/pyrules.py` (`mocks`) refuses `unittest.mock`, `mock`, `pytest_mock`, `responses`, `httpretty`, `requests_mock`, `respx` and `moto` there. Each rule has a canary under `tools/lint/canaries/` (for example `mock-library/benchmark/h.go`).
- **Search at 347263f** (all `*.go`, `*.py`, `*.ts`, `*.tsx`, `*.js`, excluding vendored environments): no import of a mocking library in any tracked test. The one `monkeypatch` use (`services/model/tests/test_main_telemetry.py:37`) sets a process environment variable for the default app; it replaces no collaborator.
- **Test doubles that are not mock libraries, listed for the reviewer's decision (not changed here; each is outside integration and benchmark packages):**

| # | Where | What it is | Why it is listed |
|---|---|---|---|
| 1 | `services/core/platform/egress/egress_test.go` (`fakeResolver`) | A hand-written DNS resolver returning fixed answers to the callback egress guard | A resolver answer is a fault (DNS rebinding, NXDOMAIN) produced by the test, not by a Tushe FaultPlan or the hostility proxy (AGENT-12). It is a unit test of one interface, but the rule says faults come from FaultPlans or the proxy. Owner of SEC-05 to say whether this stays as a unit-level exception. |
| 2 | `services/core/trust/decide/proposer_swap_test.go` | A canned-proposal stub (Proposer A) and recorded provider replies (Proposer B) | Recorded replies are labelled and the real `EscalateCase` runs on mock-case; the stub is a test input, not a mocked failure mode. Listed because it is the largest double. |
| 3 | `services/core/adapters/synthetic_tushe/case_client_test.go` | Exchanges recorded from the real mock-case, labelled SYNTHETIC | A recording, not a mock; listed so the label check (AGENT-25) is visible. |

The end-to-end packages (`adapters/outcome_e2e`, `adapters/synthetic_tushe` fault tests) use the real mock-case, the real hostility proxy and real Postgres (`testpg`), per the stack table in AGENTS.md §6.

## 2. No benchmark-only branches (REQ-TST-006)

- Searched the production code of `services/core` and `services/model` for benchmark, oracle, held-out and profile switches. `services/core` has none: its only environment reads are `NOEMA_CORE_ADDR` (`cmd/core/main.go:81`) and the test harness's database URL (`internal/testpg/testpg.go:244`). Comments that mention the benchmark describe who reads a field, not a code path.
- **One exception, for the reviewer to accept or reject: `BENCH_OUT` in `services/model/noema_model/main.py` and `platform/telemetry/__init__.py:186`.** When set (only by the bench override of the Compose stack), each model call's record is also written as a cost file (REQ-NFR-007). It adds accounting output only; it does not change any request, response, decision or provider choice. It is a benchmark-triggered branch in production code, so it is a REQ-TST-006 question even though its effect is observability. Smallest alternative if rejected: always write the cost file to a configured path and let the bench set the path (no branch on "bench"). Not changed here; changing a model-service default is not on this task's allowlist.
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
- Exceptions 1 and `BENCH_OUT` are listed, not removed: removing them changes SEC-05 and model-service files outside this task's allowlist. Each needs an owner decision (stay as a recorded exception, or change).
- Python test packages other than `services/model/tests` were searched by import only.

## 5. Suggested follow-ups (for the Steward to place; none is started)

1. A CI check that a PR whose title cites a task in a P0 path has at least one commit that adds a test before the first source commit (would make section 3 a check, not an audit). It belongs to MEM-02 or PLT-09, not here.
2. Decide the two exceptions in section 1 (row 1) and section 2 (`BENCH_OUT`) with their owners.
