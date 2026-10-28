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

## Session 2026-10-27 · ENV-08 (remediation item 3: create-if-absent in mock-case)

### Session summary
- What changed (PR links): mock-case's create is conditional on absence (`If-None-Match: *`; a unique partial index `cases_subject_epoch_uq`, migration `0003_create_if_absent.sql`; a new outcome `PRECONDITION_FAILED`, `412`, which names the case that exists and its server-issued version token; `428 PRECONDITION_REQUIRED` without the header); the core case adapter sends the header and reports a `412 PRECONDITION_FAILED` as the transport class `DUPLICATE_CREATE` (errors.v2, D6; Execution gives `REJECTED_BY_TARGET`, not `REJECTED_STALE_VERSION`) with the existing case's ID and token; `mock_case_recordings.json` re-recorded from the real mock-case (new scenario `create_precondition_failed_412`; each faulted create has a subject of its own); the linearizability model gained a rival key per subject. PR link: see the pull request that carries this file.
- Why: the founders' remediation list, item 3. `create_escalation_case` stays `CONDITIONAL_WRITE` (SoT and descriptor unchanged), so the target must honour a condition. Before this, two different keys for one transaction made two cases; after an unknown outcome (E7) a second create could therefore duplicate.
- Risks, and how they are contained: (1) The guarantee holds for the mock target only. Tushe's create semantics are an open item for the founders to confirm; nothing here is a claim about Tushe, and Tushe-dependent work stays paused. (2) "A stable idempotency key bound to the grant" is read as PERD §27.2: the key is derived from the grant's key fields (tenant, principal, transaction, logical action, reason, policy scope, epoch), not from the grant ID, so a retry under a new grant sends the same key. If the founders meant a key that includes the grant ID, a retry would be a different key and would be refused with 412; say so and it changes. (3) The header is required (428 without it) rather than optional, so an unconditional create has no route. It can be relaxed in one place (`parseCreateCase`). (4) Absent means "no OPEN or ROUTED case for the tenant, transaction and epoch"; nothing in mock-case closes a case, so a subject can never be created twice at one epoch.
- Tests added (REQ, INV, US or EC IDs): REQ-EXE-004, REQ-ENV-008, US-06, US-19, EC-E02: header required; duplicate create; conflicting creates (same key other payload `409`, other key same subject `412`, other epoch, transaction and tenant are other subjects); 64 concurrent creators with different keys (one `201`, 63 `412` naming the winner, gapless references); 32 concurrent repeats of one key; read-back (find by key, get by ID, fabricated token refused with `409 VERSION_MISMATCH`, the token read back routes); the database refuses a second open case directly; the linearizability workload with a rival key (and a corruption canary); adapter contract tests for the `412` (and for any other `412`); an end-to-end race between E7 and E8 against the real mock-case (`REJECTED_BY_TARGET`, class `DUPLICATE_CREATE`, one case).
- Regression cases added: the re-recorded wire exchanges; the rival-key linearizability model.
- Debt entries: none written (docs repository). Suggested: the controller has no path that turns `REJECTED_STALE_VERSION` into a RecoveryRequest yet (EXE-04).
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the founders confirm the three readings above and Tushe's create semantics; the benchmark owner checks that no benchmark case or fixture creates two cases for one transaction and epoch.
- Open questions (owner): Tushe's create semantics (founders); the grant-bound key reading (founders).
- Files to read first: `synthetic/mock_case/README.md` ("Create if absent"), `internal/store/ops.go` (`CreateCase`), `services/core/adapters/synthetic_tushe/case_client.go` (`classifyCreate`).
- Do not touch: the unique index (the guarantee rests on it), the recordings (re-record with the real service, never by hand).

## Session 2026-10-28 · ENV-08 (the proposer swap test)

### Session summary
- What changed (PR links): a stub-based proposer swap test in two linked halves, plus the proposers. `services/model/tests/{support_swap,support_contract,test_proposer_swap}.py` and `tests/fixtures/proposer_swap/canned_outputs.json` (proposer A, a canned-proposal stub; proposer B, the OpenAI Responses adapter replaying a canned provider response; no network); `synthetic/proposer_swap/` (the proposals they produce, and `decisions.json`); `services/core/trust/decide/proposer_swap_test.go` (decision half); `services/core/execution/e2e/proposer_swap_test.go` (execution half). No core code, no `tools/lint` change. PR link: see the pull request that carries this file.
- Why: the founders asked for evidence that the core decides and records the same for the same canned decision whatever proposer produced it, and refuses an invalid or prohibited proposal the same way. The replay of Luna's recorded outputs was dropped (the diagnostics hold no raw responses).
- Risks, and how they are contained: (1) **This validates the decision path only. It does not validate the unbuilt intake or the complete architecture.** The core has no code that parses an Intent Object; `intake` in the decision half is a stand-in for the Trust intake, not the intake, and fixes the three authority layers to PASS (the authority and policy engines are not built). (2) The Intent Object follows SoT §43.1 and PERD §20.1 (`contracts/schemas/intent-object.v1.json`); no field was invented; the four valid Intent Objects pass `tools/contracts/validate.py`. (3) A single chained Go test was not possible: `import-boundary` forbids execution reaching trust, and `import-adapters` limits adapters. The founder chose two linked tests; they meet at the committed `decisions.json`, which the decision half fails on if it is stale, and the proposal files are guarded the same way by the model test. (4) Normalisation in the execution half covers only values drawn from entropy or mock-case's own clock (ULID entropy of minted ids, the hashes and receipt signature that cover them, mock-case's case id, reference and time); the chain is verified in every run.
- Tests added (REQ, INV, US or EC IDs): REQ-TST-004, REQ-ENV-008, INV-01, INV-02, INV-03, INV-12, INV-21: same decision gives identical outcome, Record events, workflow result and mock-case state under both proposers; a prohibited remedy (T4, PROHIBITED_CAPABILITY) and an invalid proposal (T1, INTENT_INVALID) are refused identically and write nothing; mutation controls in both halves (provenance in the decision, in the ambiguity flags, in a grant parameter) that must be noticed; drift guards on the committed files.
- Regression cases added: the committed proposals and decisions under `synthetic/proposer_swap/`.
- Debt entries: none written (docs repository). The stand-in stays until the Trust intake exists; replace it then.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: when the Trust intake is built, replace `intake` with it and keep the two halves' comparisons.
- Open questions (owner): none.
- Files to read first: `synthetic/proposer_swap/README.md`.
- Do not touch: the drift guards; regenerate with the commands in that README, never by hand.

## Messages

### 2026-10-25 (time not recorded) accepted · Founder → Session B
- Purpose: "Session B: your work is complete. Hold. Do not start new work or touch any branch until the founder gives the next task. Do not push or re-run anything."
- Receiver used it for: held; did nothing until the next task below.
- Evidence: none (no repository change).

### 2026-10-27 (time not recorded) accepted · Founder → Session B
- Purpose: "Session B: TestMockCaseHistoriesWithoutFaultsAreLinearizable (synthetic/mock_case/internal/api/linearizability_test.go:421) failed once on main with '3 operations lost their response with no fault loaded', then passed on re-run. The likely cause is a client timeout under concurrent load against real PostgreSQL. Fix it in the test's workload or timeout, not by loosening the check. Open one PR, report the head SHA and check results with run links. Do not re-run any job on main. Log every message in status/. Do not merge."
- Receiver used it for: reproduced the failure locally (old 300 ms wait, one loaded core), changed the quiet run's client wait to 10 s, added the loss reasons to the failure message, opened one PR from `sB/t3-linearizability-flake`; no job on `main` was re-run; this entry.
- Evidence: the PR that carries this file (its check runs are linked in the PR body, at its final head).

### 2026-10-27 (time not recorded) accepted · Founder → Session A
- Purpose: "Session A: #77 stays open and unmerged. Do three things, in order. Log every message in status/. Do not merge anything. The non-author co-founder approves each PR, and the founder merges. PR 1: contract (remediation items 1 and 2). ... PR 2: create-if-absent in mock-case (remediation item 3). Start after PR 1 is open; it merges after PR 1. Keep create declared CONDITIONAL_WRITE. Do not change the Source of Truth or the descriptor to NONE. Implement target-side create-if-absent: a stable idempotency key bound to the grant, the precondition enforced atomically in the target, and conflicting or duplicate attempts rejected with the right error. Read-back state must be verifiable. Do not use a fabricated version token. Tests must prove the guarantee and its failure cases: duplicate create, conflicting create, concurrent create under load, and read-back verification. If you hit a real technical limit, stop and report the specific trade-off. Do not silently change the contract. In the PR body, state that the guarantee holds for the mock target only, and that Tushe's create semantics are an open item for the founder to confirm. Step 3: #77 (after PR 1 merges). Merge origin/main into #77. Update its writers, readers and verifiers to the new schema. No rebase, no force-push. Rerun checks once on the final head, and report the head SHA and results with run links. Keep #77 open for the co-founder's approval. Do not merge it." (PR 1 part: see `status/CON-01.md`.)
- Receiver used it for: PR 2 (this PR). Step 3 could not be done as written: #77 was already merged (27 Oct 2026, 13:14 UTC) before the instruction, so there is no open #77 to merge main into; PR 1 updates the writers, readers and verifiers on `main`'s code directly. Reported to the founder in the PR 1 and PR 2 reports.
- Evidence: the PR that carries this file.

### 2026-10-28 (time not recorded) accepted · Founder → Session B
- Purpose: "Session B: start the replay test. Work in noema on a branch from main. Do not merge. 1. Check whether the artifact from run 40577810935 (run-OPENAI_GPT_6_LUNA.json and diagnostics) holds the raw model responses for each call. Report exactly what it holds, with the file and field names. If it holds the raw responses, go on to step 2. If not, stop and report. 2. Build an integration test that feeds Luna's recorded responses through core end to end, using the existing mock-case. Use no API calls. 3. In the same test, run a second proposer on the same cases, such as a deterministic stub that returns the same proposals. Assert that outcomes and Record events are identical, and that core code is unchanged between the two proposers. 4. Report the head SHA, the file list, and the check run links on the PR. Do not merge. Log every message in status/ENV-08.md."
- Receiver used it for: step 1 only. The artifact (`bench-live-out-full-40577810935`) is on a host denied by the session's network policy; nothing was opened and its contents are not confirmed. Steps 2 to 4 were not started.
- Evidence: none in the repository (a report to the founder).

### 2026-10-28 (time not recorded) accepted · Founder → Session B
- Purpose: "Session B: the replay plan is dropped. Diagnostics and run files hold no raw responses, so the replay of Luna's outputs can't be built. Do not open the artifact host. Do not wait on the artifact. Build the stub-based swap test on a branch from main, with no network calls and no API spend: 1. Proposer A: a stub that returns a canned, valid proposal. 2. Proposer B: the OpenAI Responses adapter, replaying a canned provider response, with no network. 3. Assert that for the same canned decision, core produces identical outcomes and Record events under both proposers. 4. Assert that for an invalid or prohibited proposal, core refuses it identically under both proposers. 5. Make no changes to core code. The test must fail if core behaves differently depending on which proposer produced the proposal. Report the head SHA, the file list and the check run links. Do not merge. Log every message in status/ENV-08.md."
- Receiver used it for: the swap test of this PR. Found that the core has no proposal intake, so asked the founder (question: how to bridge it; options were Python proposers with test-only Go glue, or a chained Go test).
- Evidence: this PR.

### 2026-10-28 (time not recorded) sent · Session B → Founder
- Purpose: the question above: the core has no intake, so what stands between a proposer's output and `decide`? Recommended: Python proposers plus test-only Go glue.
- Receiver used it for: answered "A: Python proposers + test-only Go glue".
- Evidence: none in the repository.

### 2026-10-28 (time not recorded) accepted · Founder → Session B
- Purpose: "Session B: option A is approved, with these requirements. 1. The Go glue lives only in test files. Core packages must not import it. Label it in the file header as a stand-in for the Trust intake, not the intake itself. 2. Derive the Intent Object shape from the PERD and the source-of-truth document. Do not invent fields. Cite the sections. 3. Proposer A: a canned-proposal stub. Proposer B: the OpenAI Responses adapter replaying a canned response, with no network. 4. Every proposer's output passes through the same admission, policy and execution checks. The test uses the real decide.Admit and FailAtGate, and the real EscalateCase workflow on mock-case. Do not mock the decision outcomes or simulate writes. 5. Mutation check: if proposer provenance leaks into core's result, the test must fail. Show that it fails. 6. State in the test header and the PR body: this validates the decision path only. It does not validate the unbuilt intake or the complete architecture. 7. No changes to core code. No network calls. No API calls. Report the head SHA, the file list and the check run links. Do not merge. Log every message in status/ENV-08.md."
- Receiver used it for: the design of this PR (items 1 to 7); the mutation evidence is in the PR body.
- Evidence: this PR.

### 2026-10-28 (time not recorded) sent · Session B → Founder
- Purpose: report that a single chained Go test cannot pass `tools/lint` (import-boundary, import-adapters, money-float-area); options: two linked tests, or a lint change.
- Receiver used it for: answered "Two linked tests".
- Evidence: none in the repository.

### 2026-10-28 (time not recorded) accepted · Founder → Session B
- Purpose: "Session B: the founder chooses option 1, two linked tests, with a drift guard. 1. The decision half in trust/decide runs the real Admit and FailAtGate on both proposers' Intent Objects and writes the decisions file. It also fails if the regenerated file differs from the committed one. 2. The execution half in execution/e2e reads the committed decisions file and runs the real EscalateCase on mock-case for each proposer. It compares outcomes and Record events. 3. Both halves keep their mutation controls. 4. Do not merge the drafted chained test at services/core/integration/proposer_swap/. Remove it from the branch. 5. Make no lint change and no core change. Report the head SHA, the file list and the check run links. Do not merge. Log every message in status/ENV-08.md."
- Receiver used it for: the two linked tests of this PR; the drafted chained test was never committed.
- Evidence: this PR.
