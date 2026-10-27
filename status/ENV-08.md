# Status · ENV-08 · session notes

Moved from `docs/sessions/` on 26 Oct 2026 (DEC-2026-10-26-session-notes-in-status). One section per session, newest last; the text of each section is unchanged (headings one level lower). Files merged: `2026-10-21-0930-ENV-08.md`, `2026-10-22-1530-ENV-08b.md`.

## Session 2026-10-21 09:30 WAT · ENV-08

### Session summary
- What changed (PR links): `synthetic/mock_case/` built (T3 of DEC-2026-10-20); additive rows in `deploy/compose` and `netcheck/matrix.py`; `Makefile` targets `env-mock-case-*`; workflow `mock-case.yml`. PR link: see the PR that carries this file.
- Why: every POC write targets a case system and Tushe has none (DEC-2026-10-12 D1); REQ-ENV-008 asks for a synthetic one, built by someone other than the builder of `services/core`.
- Risks, and how they are contained: (1) the compose change touches the shared stack, so mock-case is outside the `dev` profile and `make up` is unchanged; the matrix starts it itself. (2) A new dependency, `github.com/jackc/pgx/v5` v5.8.0, in the mock-case module only (AGENT-14): it needs approval. (3) The Compose runtime path could not be run in this session (no Docker daemon); `docker compose config` validates, and the binary, `db-init.sh` and the credential step were run against a real PostgreSQL by hand.
- Tests added (REQ, INV, US or EC IDs): REQ-ENV-008 contract tests for every §34.4 row; US-05, US-06, US-19; EC-E01, EC-E02, EC-E05, EC-E06, EC-E11, EC-E12, EC-E13, EC-E14, EC-K01 as fault tests; REQ-ENV-002 truth refusal; REQ-ENV-004 replay; D7 linearizability.
- Regression cases added: none (no bug found in product behaviour). Two test defects were found and fixed while writing: a refusal test that passed for the wrong reason (version not advanced), caught by mutation; the linearizability canary is the committed corrupted-history test; separately, by hand, a seeded bug (a route that ignores the version) was confirmed to make the workload fail, then removed.
- Debt entries: none written (the docs repository is session A's). Suggested: testcontainers is replaced by a CI service container and a throwaway local server (docker is not available here).
- Conflicts raised: none blocking. Observations for session A are listed in the PR body under "Needs session A".

### Pre-work for the next agent
- Exact next step: this session's list continues with the fault-plan schema (`contracts/schemas/fault-plan.v1.json`), then the hostility proxy (T4 item 7) and the webhook relay (T4 item 8). The fault-plan schema should adopt the operation names, match keys, effects and params in `synthetic/mock_case/README.md`, or `internal/faultplan` changes with it.
- Open questions (owner): approval of `pgx/v5` (founders, AGENT-14); whether `dev` joins mock-case's profiles now or with the core's case adapter (session A).
- Files to read first: `synthetic/mock_case/README.md`, `internal/faultplan/plan.go`, `internal/store/migrations/0001_init.sql`, `deploy/compose/services/mock-case.yml`.
- Do not touch: `services/**`, `benchmark-heldout`, any threshold.

## Session 2026-10-22 15:30 WAT · ENV-08 (follow-up: the five missing case-system effects)

### Session summary
- What changed (PR links): mock-case produces `NEAR_TIMEOUT_BEFORE`, `NEAR_TIMEOUT_AFTER`, `HTTP_200_SEMANTIC_PENDING`, `IDEMPOTENCY_COLLISION` and `INJECTION_PAYLOAD`; `fault-plan.v1.json` allows them on target `mock-case` (relaxation, migration note in `contracts/INVENTORY.md`).
- Why: session C's generator marks these effects `needs_fault` because mock-case could not produce them (REQ-ENV-008, REQ-ENV-004, REQ-TST-004).
- Risks, and how they are contained: the semantics of the near-timeouts, the pending body and the injection text are this session's reading (PR body, "Agent decisions"). Every effect is injected on the real wire; a plan that names one on an operation it cannot apply to is refused, never skipped.
- Tests added (REQ, INV, US or EC IDs): EC-T09 (both near-timeouts), EC-E03 (pending, three keyed writes), EC-E06 (collision), EC-X02 and AGENT-09 (injection on four reads); plan-loader refusals; schema fixtures (1 valid extended, 5 invalid).
- Regression cases added: none (new behaviour).
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: session C removes the `needs_fault` markers for these five effects once this merges, and renames `creates_case` away (the pending effect takes no params).
- Open questions (owner): whether the benchmark owner wants a different reading of the near-timeouts (session A / founders).
- Files to read first: `synthetic/mock_case/README.md` (effects table), `internal/api/pipeline.go`.
- Do not touch: the stored data path of `INJECTION_PAYLOAD` (it must stay answer-only).

## Session 2026-10-27 · ENV-08 (follow-up: a flaky quiet-run linearizability test)

### Session summary
- What changed (PR links): `TestMockCaseHistoriesWithoutFaultsAreLinearizable` (`synthetic/mock_case/internal/api/linearizability_test.go`) now gives its clients 10 s to answer (`quietClientTimeout`) instead of the 300 ms the fault runs use (`faultClientTimeout`), and its failure message lists why each operation lost its response. The check itself is unchanged: `rec.n.pending` must still be 0. PR link: see the pull request that carries this file.
- Why: the test failed once on `main` ("3 operations lost their response with no fault loaded") and passed on re-run. A quiet run has no fault that can lose a response, so the only way to lose one is a client that gives up before a slow machine answers.
- Evidence of the cause: reproduced locally with the old 300 ms wait. With the test, its PostgreSQL and six busy loops pinned to one core (`taskset -c 0`, `-race -count=10`), 2 of 10 runs failed; every lost response was `POST /v1/cases: context deadline exceeded (Client.Timeout exceeded while awaiting headers)`, and there was no 5xx. With 10 s, the same load gave no lost response in 25 runs (see the PR body for the exact result).
- Risks, and how they are contained: the fault runs (`TestACorruptedHistoryIsRejected`, the seeded and sequential tests) keep 300 ms because their faults (a 600 ms hold) depend on it. A real service stall of more than 10 s would still fail the quiet test, as it should.
- Tests added (REQ, INV, US or EC IDs): none; REQ-TST-002 and D7 linearizability coverage is unchanged and now deterministic under load.
- Regression cases added: none beyond the reproduction above (a CPU-starved run), which is not a CI test.
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the founder reviews and merges the PR.
- Open questions (owner): none.
- Files to read first: `synthetic/mock_case/internal/api/linearizability_test.go` (`workloadWithTimeout`, `quietClientTimeout`).
- Do not touch: the `rec.n.pending != 0` assertion (do not loosen it).

## Messages

### 2026-10-25 (time not recorded) accepted · Founder → Session B
- Purpose: "Session B: your work is complete. Hold. Do not start new work or touch any branch until the founder gives the next task. Do not push or re-run anything."
- Receiver used it for: held; did nothing until the next task below.
- Evidence: none (no repository change).

### 2026-10-27 (time not recorded) accepted · Founder → Session B
- Purpose: "Session B: TestMockCaseHistoriesWithoutFaultsAreLinearizable (synthetic/mock_case/internal/api/linearizability_test.go:421) failed once on main with '3 operations lost their response with no fault loaded', then passed on re-run. The likely cause is a client timeout under concurrent load against real PostgreSQL. Fix it in the test's workload or timeout, not by loosening the check. Open one PR, report the head SHA and check results with run links. Do not re-run any job on main. Log every message in status/. Do not merge."
- Receiver used it for: reproduced the failure locally (old 300 ms wait, one loaded core), changed the quiet run's client wait to 10 s, added the loss reasons to the failure message, opened one PR from `sB/t3-linearizability-flake`; no job on `main` was re-run; this entry.
- Evidence: the PR that carries this file (its check runs are linked in the PR body, at its final head).
