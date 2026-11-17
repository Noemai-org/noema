# Status · UND-16 · the compile acceptance run

Owner: C2 · Task: UND-16 (IL, C2): the live compile acceptance run (A198); noema#546, noema#569 (the workflow, merged), noema#654 (this PR).

# Session 2026-11-17 · UND-16 · the runner (WIP exception A253)

## Session summary
- What changed (noema#654): `benchmark/compile_acceptance/` (the golden examples, the recorder, the run loop, the command, the live build from the model service, the leak-scan allow file) and `services/model/tests/test_compile_acceptance_build.py`.
- Why: noema#569 calls `python -m benchmark.compile_acceptance` and `benchmark/compile_acceptance/leakscan.allow`; neither existed, so its dry run failed at the first call. The recordings are UND-15's source (A261).
- Risks, and how they are contained: it spends real money only inside a Founder-dispatched job; the budget guard decides before each request, a stop stays, a failed call is settled at its projection, and a run of no provider call is exit 3, never a pass. The key is read only by the model service's provider adapter. No host and no header is written. The first live reply is not yet seen by this code: stated in the README.
- Tests added (REQ-INT-001, REQ-INT-002, INV-22, INV-29, AGENT-12): 40 benchmark tests and 9 model-service tests; 19 hand mutants over the runner, build, recorder, command and check, all killed (two survived the first tests and each got a case).
- Regression cases added: the stop in the last example, a stop reached inside the transport, a refusal of the last example, a later larger request of an example not replacing the first price, a unit that is not NGN.
- Debt entries: `build.py` repeats the pack view and the minter of noema#635 (`wiring.py`); replace them by an import once noema#635 is on `main` (owner C2, task UND-16).
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after this merges and the Founder dispatches the dry run, read its cost; then the paid run; then UND-15 reads `recordings/manifest.json` and the request hashes.
- Open questions (owner): the model asked is the frozen `gpt-6-luna` configuration (`--harness-config`, default); a Founder who wants `gpt-6.1-terra` (about ten times the rate) passes the other file.
- Files to read first: `benchmark/compile_acceptance/README.md`, `.github/workflows/bench-live-compile.yml`.
- Do not touch: `benchmark/decisions/harness-*.json` (frozen).

# Session 2026-11-17 · UND-16 · Codex P2 on noema#654 (the dry-run exit code, A285)

## Session summary
- What changed (noema#654): `--dry-run` now exits 3 (NOT RUN), not 0 (`NOT_RUN` constant in `__main__.py`); the module docstring and the README exit table say so; `test_cli.py` expects 3 (and a new case: 3 with and without a key).
- Why: Codex's P2 on `__main__.py:82` and the Architect's A285 ("the code follows the documented exit meanings"). The documented exit 3 is "no key, or no provider call was made: never a pass"; a dry run calls no provider, so a script that gates on the exit code could read an unrun acceptance as green.
- Risks, and how they are contained: `.github/workflows/bench-live-compile.yml` (noema#569) runs the dry run under `set -euo pipefail | tee`, so exit 3 turns its `dry-run` job red and the `run` job (`needs: dry-run`) would never start. That workflow is not in this pull request's allowlist; the change that lets the dry-run job treat exit 3 as "dry run complete" is a separate pull request (SR reads it). Until it merges, do not dispatch the dry run from `main`.
- Tests added (REQ-INT-001; AGENT-11): the dry-run case now asserts 3; one new case asserts 3 for both an empty and a keyed environment. Red first: both failed with `0 != 3` before the change.
- Regression cases added: the two above.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the workflow change (dry run accepts exit 3 and still fails on 1 or 2), then the Founder's word for the dry run.
- Files to read first: `.github/workflows/bench-live-compile.yml`, `tools/ci/test_bench_live_compile.py`.
- Do not touch: `benchmark/decisions/harness-*.json` (frozen).
