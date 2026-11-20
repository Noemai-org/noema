# Status · BEN-05 package artifacts (Benchmark Owner agent) · the runner's per-case write counts and the stamps

# Session 2026-11-20 · BEN-05 · A444

## Session summary
- What changed: `benchmark/runner/package_artifacts.py` and 16 tests. `python -m benchmark.runner.package_artifacts --run DIR --sha SHA --run-id ID [--probes FILE]` writes, into the run directory, `mock-case-writes.json` (`{"stamp": {"sha","run_id"}, "writes": {case: integer}}`) and `stamp.json`, and with `--probes` adds the stamp to `negative-probes.json`.
- Why: ruling A444 (20 Nov): the runner's oracle client may give per-case write counts (aggregates only, from the mock-case truth listener with the runner's own credential), and the dispatch SHA and run id are stamped into every artifact. IL said the mock-case log carries no write records, so the count comes from where the runner already records it: each case result's `environment.effects` (`benchmark/oracle_client/effects.py`, read from the truth listener). This module reads those results and adds no new reader and no new credential.
- Fail closed: a case whose result has no environment block, a malformed effect, a count that is not a whole number of at least zero (a boolean or a text count included), or two results, gets **no entry** (never a zero) and the exit code is 1; an empty run, an unreadable result or one with no case id is an error (exit 2, nothing written). The SHA must be a full lowercase 40-hex SHA and the run id 1 to 64 letters, digits, dot, dash or underscore. A stamp already in the probes file for another dispatch or run is never overwritten.
- Not done: `requests.jsonl` (the runner's per-request record: the writer needs the HTTP client instrumented) and `reconstructions.json` (three phases fetched through the audit route after a recovery sweep and a replay); both are still mine and still open.
- Tests: 16, written first; 15 mutations, 14 caught and 1 equivalent (the CLI's early stamp check; `stamp_doc` checks before anything is written); mypy --strict and lint clean.
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: the runner writer for `requests.jsonl` and the fetch for `reconstructions.json`; IL's workflow calls this module after the run and uploads the files under `benchmark/out/run/` and `benchmark/out/evidence/`.
- Do not touch: the package's input formats (`benchmark/suites/g1_package.py`).
