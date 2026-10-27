# Status · BEN-03 (Session C) · runner, provider cost

Updated 2026-10-25. Owner: Session C. Related tasks: BEN-10 (generator, `status/BEN-10.md`), UND-02 (Session D, cost files), BEN-12 (scorer).

## State

- The runner reads provider cost from the model service's bench cost files (`--cost-dir`; `make bench` passes `$(BENCH_OUT)/cost`), layout `<correlation_id>-<NNN>.json`, each the JSON of `CallRecord.to_dict()`. `benchmark/runner/cost.py`; tests in `test_cost.py` and `test_run.py`.
- A case's `cost_usd_micros` is the sum of its calls' `cost.total_micro`. It is null ("not available") when there is no directory, no file, or any file is unreadable, incomplete, not an integer or not USD. Never estimated, never converted, never read from the reconstruction.
- The scorer and report are unchanged in behaviour (null in, null out); wording no longer says "no source exists".

## Open

- Not read end to end: the model service writes no file until its request path constructs the harness (`status/UND-02.md`). The tests use records written by hand to the service's shape.
- Currency: `cost_usd_micros` is millionths of a US dollar. A provider rate card in another currency makes the case null. If the founders want a conversion, that is a decision for the spec first (rate source and date); none is invented here.
- `BEN-INJ-0003` is unchanged and keeps its `needs_fault` marker until the founder confirms the move to `reason_code`.
- Bake-off: not run, no access.

## Messages

### 2026-10-25 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: the cost file name is `<correlation_id>-<NNN>.json`; start the cost reader against that layout; do not change `BEN-INJ-0003` yet; report the PR link and check results with run links on the final head; log every message in `status/`; do not merge.
- Receiver used it for: this change (`cost.py`, `--cost-dir`, tests, READMEs).
- Evidence: this PR.

### 2026-10-24 (before this branch) accepted · Founder (relayed by the user) → Session C
- Purpose: the cost route: the model service writes each call's cost to `$(BENCH_OUT)/cost/<call_id>.json` in the `CallRecord.to_dict()` form; read it from there; do not import `services/**`; add no public endpoint; report cost as null until the file exists on `main`.
- Receiver used it for: the design of `cost.py` (a directory read, no import, no endpoint). The file name was then settled by the instruction above.
- Evidence: noema#62 (the writer wiring), this PR.

### 2026-10-27 (time not recorded) accepted · Founder (relayed by the user) → Session C
- Purpose: #64 failed because its branch predated the scan-scope fix (#69); `origin/main` is to be merged into `sC/t4-cost-reader` without rebase or force-push, the result pushed, checks run once on the new head, and the head SHA and each check's result reported in one table with run links on #64; the founder merges after the checks pass; every message is logged in `status/`.
- Receiver used it for: the merge of `origin/main` into this branch (no conflicts) and this entry.
- Evidence: #64.
