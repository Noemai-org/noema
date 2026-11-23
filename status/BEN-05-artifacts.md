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

# Session 2026-11-20 (second) · BEN-05 · the runner's `requests.jsonl` and `reconstructions.json` (A444)

## Session summary
- `requests.jsonl`: the runner now writes its own per-request record, in the format agreed with IL (one object per line: `case_id`, `correlation_id`, `method`, `path`, `status` as an integer, `kind`). `benchmark/runner/request_log.py` (`RequestRecorder`), `PublicApi(recorder=...)` records every call it makes (a refusal's status too), `JsonClient.last_status` exposes the status, and `run_case`/`run_suite` begin and end each case and write the file. **Only** the reconstruction route is kind `reconstruction`; the rest are `case`; `probe` is for the evidence probes (not written yet: see below). No header, body or token is recorded, and `path` is the route TEMPLATE (`/v1/reconstructions/{correlation_id}`): a concrete path is refused, so no identifier (and no digit run for the leak scan) can reach the file. A case that never got a correlation id records nothing (never an empty correlation).
- `reconstructions.json`: the runner writes `{case: {"initial": <reconstruction body>}}` for every completed case; `package_artifacts` wraps it as `{"stamp": {...}, "cases": {...}}` (it must exist; a stamped file for another dispatch or run is never overwritten).
- Tests: `test_request_log.py` (13), `test_run_artifacts.py` (4), `test_package_artifacts.py` (21); the existing run, API, evidence, CLI and oracle tests still pass (the `test_idp_session` failures and the two benchmark-wide mypy errors are Windows-only and identical on a clean main checkout). 12 mutations of the recorder, the API instrumentation and the run loop: 10 caught at once, 2 weak assertions found and fixed (now caught). The run-artifacts tests were written after the instrumentation, not before; the mutation run is what shows they detect breakage.
- **Not done, and why:** (1) the evidence probes' own lines (kind `probe`, and the control read as kind `reconstruction`): they are made by `benchmark.runner.evidence` in IL's separate step, so that step has to append to `requests.jsonl`; without them the core's RECORD_ACCESSED log would hold more SERVED lines than the run's own record, so IL's count check would fail a correct run. Next. (2) The `after_recovery` and `after_replay` phases of `reconstructions.json`: I do not know what triggers the recovery sweep and the replay or which role reads afterwards; asked EL and IL. The package ends NOT MET on row 2 without them, by design.
- Regression cases: none. Debt: none. Conflicts: none.

# Session 2026-11-20 (third) · BEN-05 · the probes' own request lines, appended by `package_artifacts`

## Session summary
- The evidence probes read the reconstruction route too, so the core's RECORD_ACCESSED log holds their lines; the run's own record must hold the same reads or noema#730's count check would fail a correct run. `package_artifacts --probes FILE` now appends, from `negative-probes.json` itself, one `reconstruction` line for each case's control read (the case's own correlation, as the core logs it) and three `probe` lines for the refusal checks (the unknown correlation is the fixed placeholder `corr_unknown_probe`; the real one is random and outside the run); a read that got no answer adds no line; running it twice adds nothing; a missing request record or an unreadable probe file is an error before anything is written. This is done here so `benchmark.runner.evidence` (which IL is also changing on noema#730) is not touched.
- A stamped `reconstructions.json` must be exactly `{"stamp", "cases"}` with this stamp; anything else is refused, never overwritten.
- Tests: 28 in `test_package_artifacts.py`; 13 mutations of the new logic, 12 caught and 1 equivalent (a missing request file raises anyway).

# Session 2026-11-20 (fourth) · BEN-05 · the command builds the recorder (EL's finding on noema#821)

## Session summary
- EL's read of noema#821 found that `python -m benchmark.runner` built `PublicApi(args.api_url)` with no recorder, so a real run never wrote `requests.jsonl` and noema#730's full mode would end NOT RUN. Fixed: the command builds `PublicApi(args.api_url, recorder=RequestRecorder())`. The unit tests of #821 drove the run loop with a recorder they built themselves, which is why they did not see it; the new test goes through the command (red on main, green with the fix). 23 `test_cli` tests pass; mypy --strict and lint clean.
- Not done: the `after_recovery` and `after_reread` phases (ruling A476): a separate PR.

