# Status · BEN-03 (Session C) · runner, provider cost

Updated 2026-10-25. Owner: Session C. Related tasks: BEN-10 (generator, `status/BEN-10.md`), UND-02 (Session D, cost files), BEN-12 (scorer).

## State

- The runner reads provider cost from the model service's bench cost files (`--cost-dir`; `make bench` passes `$(BENCH_OUT)/cost`), layout `<correlation_id>-<NNN>.json`, each the JSON of `CallRecord.to_dict()`. `benchmark/runner/cost.py`; tests in `test_cost.py` and `test_run.py`.
- A case's `cost_usd_micros` is the sum of its calls' `cost.total_micro`. It is null ("not available") when there is no directory, no file, or any file is unreadable, incomplete, not an integer or not USD. Never estimated, never converted, never read from the reconstruction.
- The scorer and report are unchanged in behaviour (null in, null out); wording no longer says "no source exists".

## Open

- Not read end to end: the model service writes no file until its request path constructs the harness (`status/UND-02.md`). The tests use records written by hand to the service's shape.
- Currency: `cost_usd_micros` is millionths of a US dollar. A provider rate card in another currency makes the case null. If the founders want a conversion, that is a decision for the spec first (rate source and date); none is invented here.
- `BEN-INJ-0003` keeps its `needs_fault` marker. A separate proposal PR covers the move to `reason_code`; the marker changes only after the benchmark owner or the non-author co-founder approves it.
- Bake-off: not run, no access.

## Messages

### 2026-10-25 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: the cost file name is `<correlation_id>-<NNN>.json`; the cost reader is to start against that layout; `BEN-INJ-0003` was not to change yet; the PR link and check results with run links on the final head were to be reported; every message is logged in `status/`; the PR is not merged by Session C.
- Receiver used it for: this change (`cost.py`, `--cost-dir`, tests, READMEs).
- Evidence: this PR.

### 2026-10-24 (before this branch) accepted · Founder (relayed by the user) → Session C
- Purpose: the cost route: the model service writes each call's cost to `$(BENCH_OUT)/cost/<call_id>.json` in the `CallRecord.to_dict()` form; the reader reads it from there; `services/**` is not imported and no public endpoint is added; cost is reported as null until the file exists on `main`.
- Receiver used it for: the design of `cost.py` (a directory read, no import, no endpoint). The file name was then settled by the instruction above.
- Evidence: noema#62 (the writer wiring), this PR.

### 2026-10-26 14:28Z accepted · Session A → Session C
- Purpose: relays a founder instruction of 26 Oct to Session A to keep instruction or prompt text out of PR titles, bodies and status files; Session A found nothing to remove in the title and body of #64 and said two lines of this file carry imperative wording.
- Receiver used it for: this file now states what was received and done, without imperative wording; applied in this PR.
- Evidence: this PR.

### 2026-10-27 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: decisions are final. (1) `BEN-INJ-0003`: a PR proposing the move from `case_note` to `reason_code`, with evidence that the injection test, attack surface, fixture meaning and expected outcome are unchanged; the marker stays until the benchmark owner or the non-author co-founder approves. (2) Cost file naming: canonical name `<correlation_id>-<NNN>.json`; a correlation id with a character outside `[A-Za-z0-9_-]` is rejected and not normalised; NNN is 001 to 999 and the 1000th call is refused; files are created exclusively; the same rules in producer and reader; tests for multiple calls, colliding ids, call 1000 and no overwrite. One PR for item 2, with test results.
- Receiver used it for: this PR (producer `CostFileTelemetry`, reader `cost.py`, tests, READMEs). The producer is in `services/model`, which Session D owns; this instruction is the reason it is changed here.
- Evidence: this PR.

### 2026-10-27 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: #64 failed because its branch predated the scan-scope fix (#69); `origin/main` is to be merged into `sC/t4-cost-reader` without rebase or force-push, the result pushed, checks run once on the new head, and the head SHA and each check's result reported in one table with run links on #64; the founder merges after the checks pass; every message is logged in `status/`.
- Receiver used it for: the merge of `origin/main` into `sC/t4-cost-reader` (no conflicts), done in #64, and this entry.
- Evidence: #64.

# Session 2026-11-16 · BEN-03 · the plan for the harness's Tushe read-only fold and trace join

## Session summary
- What changed: `benchmark/oracle_client/PLAN-tushe-readonly-fold.md` and a pointer in `benchmark/oracle_client/README.md`. A plan only.
- Why: the Founder's instruction of 16 Nov 2026: plan the harness oracle fold for transitions (Tushe M-02) and the traceId join (AT-19), to read through the read-only key when the Founder creates it (A240).
- Risks, and how they are contained: none in code. The plan refuses to build a reader or name a route before a delta file adopts the addendum (DEC-2026-10-16 E10; the Steward's record of the addendum), and lists what is not known instead of guessing it.
- Tests added: none (a plan; the tests it will start with are listed in its section 4). Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after the Founders adopt the addendum's routes by a delta file, build `tushe_fold.py` and `tushe_trace.py` (pure, tests first), then the reader.
- Open questions (owner): the six in section 5 of the plan (routes and shapes, the order field, the join key, the scopes needed, time, the reply of 10 Nov): the Steward, the Founders and Tushe.
- Files to read first: `benchmark/oracle_client/PLAN-tushe-readonly-fold.md`, `benchmark/oracle_client/README.md`.
- Do not touch: no credential value is ever written in a file; the key goes in the bench Environment only.

# Session 2026-11-18 (second round) · BEN-03 · Codex P2 on the fold at 52fbc856

## Session summary
- What changed: `fold_transitions` reads every row first (an unreadable row still fails the whole answer), then applies the one-id-two-rows rule against the cutoff: if every differing row is after the cutoff the id is not part of the answer (the prefix property, PLAN-tushe-readonly-fold.md 3.2); if any is at or before it the in-prefix row is the contested one and the answer is CHAIN_BROKEN.
- Tests: 3 new, written first and failing before the fix; 8 mutations of the rule, 7 caught; the eighth (keeping a future-only conflicting id) is an equivalent mutant, since such a row never enters the prefix.
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: after Codex re-reads the fix lines (A287), the independent read, then one Founder approval. The reader waits for noema#711, the delta file and the read-only key.
- Do not touch: the row shape defaults (assumed until the delta file adopts them).

