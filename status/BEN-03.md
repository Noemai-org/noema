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
