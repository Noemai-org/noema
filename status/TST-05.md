# Status · TST-05 · crash-point matrix (design note)

Date: 2026-11-03 · Owner of the task: Integration Lead with the Security Reviewer (plan task TST-05) · Status: design, no code yet
Requirement: REQ-TST-008 (DEC-2026-10-31 D14), a G3 requirement. This file defines no requirement; the PERD does.

## Decision from the Architect (3 Nov 2026)

Option 2. The crash harness lives in the benchmark runner's side (it already holds the fault-plan client and the truth endpoint). It drives a **compiled crash-injection build of the core (a build tag)** against real PostgreSQL and the real mock-case through a FaultPlan. The Security Reviewer checks that the tag can never be built into a production image. This adds no exception to DEC-2026-10-26.

## What exists on main (checked 3 Nov 2026)

- `execution/controller`: a `Boundary func(Point) error` hook and 14 named `Point`s, one per step boundary of PERD §27.1 (`PointBeforeE1` ... `PointAfterComplete`); `ErrCrash`.
- `controller/crash_test.go` and `sim_crash_test.go`: every point under every network fault, against a **simulated** target; one crash point against real PostgreSQL in `ledger/ledger_test.go`.
- `execution/e2e`: real PostgreSQL ledger, real `CaseClient`, real mock-case, but **no crash injection and no FaultPlan** (its README keeps the control and truth endpoints out of core test code).
- `benchmark/runner/control.py` and `benchmark/oracle_client/`: the FaultPlan client and the truth client (effect counts) the harness needs.
- **`cmd/core` serves only `/healthz` and `/readyz`.** It has no request path that runs the protocol, so there is nothing in it to kill in the middle of E1 to E11 today.

## Consequence for the build tag

The tag cannot go on `cmd/core` yet. The crash build is a **separate driver**: `services/core/cmd/crashdriver`, a `package main` every file of which carries `//go:build crashinject`. It wires the real ledger, case client, bridge and controller (the same wiring as `execution/e2e/stack_test.go`), reads its crash point and the grant to execute from its environment, and sets `Controller.Boundary` to `syscall.Kill(os.Getpid(), SIGKILL)` at that point. When `cmd/core` serves the protocol, the same hook file moves under the same tag into `cmd/core`.

## Kill mechanics: two styles, both real process death

- **Self-kill at a boundary** (exact, deterministic): SIGKILL at the named `Point`. No deferred cleanup runs; the PostgreSQL connection drops and the server rolls back the open transaction.
- **External kill on an observable condition** (for "in flight" and "response lost"): the harness SIGKILLs the driver when mock-case's truth endpoint shows a condition (a command received, a command committed). It waits on observable state, never on time.

## The four REQ-TST-008 points

| # | Point | How | Must hold at the kill | Must hold after recovery |
|---|---|---|---|---|
| 1 | before the spend commits | self-kill at `PointInSpendBeforeCommit` | no `grant_spend` row; target saw nothing; effects 0 | grant still unspent; exactly one effect once executed |
| 2 | after the spend commits, before dispatch | self-kill at `PointAfterSpend` | spend row present; target saw nothing; effects 0 | no re-dispatch under the spent grant; attempt marked UNKNOWN by the sweeper; at most one effect, under a **new** grant |
| 3 | after dispatch, before the response | FaultPlan `TIMEOUT_BEFORE_COMMIT` (request held before commit); kill when truth shows the command received, not committed | spend present; no receipt; effects 0 | UNKNOWN, found by key (nothing there); a new grant gives exactly one effect |
| 4 | after the target commits, response lost | FaultPlan `TIMEOUT_AFTER_COMMIT` / `DROP_RESPONSE` / `NEAR_TIMEOUT_AFTER`; kill when truth shows committed | spend present; no receipt; effects 1 | still exactly 1; found by idempotency key (TEN-04 mechanism); no second effect |

All 14 points are run the same way as the extended matrix (the nightly of REQ-TST-009). At every sampled moment the effect count in truth must be 0 or 1.

## Seeds and the recorded run

A seed picks the subject, the idempotency inputs, the fault variant in points 3 and 4, and `delay_ms`. The harness prints one `CRASH_RESULT point=<n> seed=<s> effects_at_kill=<n> effects_final=<n> pass=<bool>` line per run and writes a run record (seed, commit SHA, FaultPlan, truth digest). Each point is repeated for N seeds (N to be set with the Security Reviewer; 20 is the proposal).

## Where it lives and the overlap with the Benchmark agent

New package `benchmark/crash_matrix/` (Python, standard library plus the existing runner clients, imported read-only). **No existing file under `benchmark/runner/` is edited**, so BEN-30 (#112, which changes the runner) cannot conflict with it; the chaos subset (TST-06) calls `python -m benchmark.crash_matrix --subset chaos`. To confirm with the Benchmark agent: the name of the hook BEN-29's `chaos` suite (#117, #138) expects.

## The Security Reviewer's control: the tag can never reach an image

Proposed, for the Security Reviewer to own and adjust:
1. Every file of `cmd/crashdriver` has `//go:build crashinject`; without the tag `go build ./...` builds nothing from it.
2. No non-tagged file may import it or reference `NOEMA_CRASH_POINT` or the kill call (an architecture-lint rule with a canary; `tools/lint` is a protected path).
3. No Dockerfile, Compose file or workflow passes `-tags` containing `crashinject`, except the one crash-matrix job (a CI check).
4. After the production image is built, `go version -m` of its `core` binary shows no `crashinject` build tag.
5. The driver refuses to start unless `NOEMA_CRASH_HARNESS=1` and the profile is `ci` or `bench`.

## What this needs, and what it does not

- **Needs, to start:** nothing outside my area for points 1, 2 and the "at most one effect" assertions (EXE-02 is on main). The driver needs a Trust stand-in to create grants: `execution/e2e/stack_test.go` has `trustStandIn` in test code; the driver would carry its own copy behind the tag, with a synthetic key.
- **Needs before "correct final state after recovery" is the real path:** OUT-01 (#136, draft) and RCV-01 (absent). Until then the recovery step uses the same stand-ins as `sim_crash_test.go` (the sweeper, a read of the target by key, a new grant from the stand-in); this is stated in every record.
- **Held:** EXE-04 (#90, #94) changes the receipt path (DEBT-01); the matrix is re-run when it merges.
- **Not covered:** Tushe. The target is mock-case. Nothing here says anything about Tushe's behaviour.

## Open questions

1. Is a copy of the Trust stand-in in tagged non-test code acceptable to the Security Reviewer (it holds a synthetic signing key, only in a `crashinject` build)? The alternative is a driver that is itself a compiled `go test` binary.
2. N seeds per point, and the CI time budget (REQ-TST-009: the chaos subset under 15 minutes per merge).

## Revision 6 Nov 2026 (Integration Lead): the harness can be a Go test under Amendment 1

**What changed since the 3 Nov design.** DEC-2026-10-26 Amendment 1 merged on 3 Nov 2026 at 04:38 UTC (docs#74): a Go test in the core tree may call mock-case's control and truth listeners with the bench credential, only in `_test.go` files, against a mock-case the test starts itself. `services/core/adapters/outcome_e2e` already holds everything the matrix needs in one package: the real Execution controller over real PostgreSQL (`newStack`), the Trust stand-in, mock-case started with its bench listener (`mockcase_test.go`), the test-only bench client (`bench_test.go`: load a FaultPlan, read the truth endpoint) and the static check that no non-test file names a control or truth path (`boundary_test.go`). The "Needs" note above (the e2e README keeps control and truth out of core test code) described the state before that merge.

**CONFLICT DETECTED, for the Architect (not a built change).** The 3 Nov decision (Option 2: a Python harness in `benchmark/crash_matrix/` driving a `crashinject`-tagged `cmd/crashdriver`) needs a new tagged driver with a copy of the Trust stand-in in non-test code, a lint rule in `tools/lint` and a crash-matrix CI job (protected paths), and PostgreSQL role setup outside the test helpers. Amendment 1 now permits the same kill-and-count evidence as a Go test with none of those.
- Option A (the 3 Nov decision, unchanged): as above. Effort high; risk: drift against a controller and logins still changing (EXE-08, EDG-01, DEBT-13); needs founder approval for the CI job and the lint rule.
- Option B (recommended): crash tests in `adapters/outcome_e2e`, runs in the existing `core` job, no new workflow, no lint rule, no non-test code. Still real process death: the test re-executes its own binary (`os.Args[0] -test.run=^TestCrashChild$`) with the crash point in the environment; the child attaches to the parent's database (the tenant logins are `<group>__<tenant>_<db>` with no password, so the child needs only the DSN), issues the grant, sets `Controller.Boundary` to `syscall.Kill(os.Getpid(), SIGKILL)` at the named point and executes. The parent then reads the database and the truth endpoint, runs `Recover`, executes under a new grant, and counts effects at every step (0 or 1, never 2). Seeds pick the subject, the fault variant and `delay_ms`. Linux only (`//go:build !windows`).
- Option C: sim tests only (what exists). Not G3 evidence.
- Recommendation: B. It keeps the property the Architect asked for (SIGKILL of a separate process, no cleanup, the server rolls back the open transaction) and the Security Reviewer's concern disappears because nothing crash-capable is in a non-test file.

**Points and what each run asserts (unchanged from the table above).** Points 1 and 2 are self-kills at `PointInSpendBeforeCommit` and `PointAfterSpend`. Points 3 and 4 add a FaultPlan (`TIMEOUT_BEFORE_COMMIT`; `TIMEOUT_AFTER_COMMIT` or `DROP_RESPONSE`) and an external kill when the truth endpoint shows the command received or committed.

**Not started, and why.** No code until the Architect picks A, B or C: Option B is a different harness from the one decided on 3 Nov (AGENT-27). Everything for B is ready to build the moment it is confirmed; I will start with points 1 and 2.