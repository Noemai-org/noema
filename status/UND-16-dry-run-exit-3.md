# Status · UND-16 · the dry-run job accepts exit 3 (A285)

Updated 2026-11-17. Plan task UND-16 (REQ-INT-001, REQ-OPS-003). Follows noema#654, where `--dry-run` now exits 3.

## State
- `.github/workflows/bench-live-compile.yml`: the `dry-run` job treats exit 3 (NOT RUN, no provider call) as "dry run complete" and fails on any other code, including 0. `tools/ci/test_bench_live_compile.py` runs the real text of the call and the check with the runner replaced by a stub that exits 0, 1, 2, 3 or 4.

## Open
- Merge after noema#654 (until then the runner exits 0 on a dry run and this job would fail it). Both must be on `main` before the Founder dispatches the dry run.

## Session 2026-11-17 · UND-16

### Session summary
- What changed: the workflow step and its guard test (above).
- Why: A285 (the code follows the documented exit meanings) makes the runner's dry run exit 3. The job runs it under `set -euo pipefail` and `tee`, so exit 3 would turn the job red and the `run` job (`needs: dry-run`) would never start.
- Risks, and how they are contained: the job now fails on exit 0 from the dry run, which a runner that no longer follows A285 would give; that is the intent. The `run` job, the Environment and the secret are untouched. `.github` change: Security Reviewer and a Founder.
- Tests added (REQ-OPS-003; AGENT-11): `test_the_dry_run_accepts_exit_3_and_no_other_code` (five exit codes). Red first: it failed with "no `code=0` guard". Mutants: guard `== 0`, no `PIPESTATUS` capture and guard `>= 3` each fail it.
- Regression cases added: the test above.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: merge noema#654, then this; then the Founder dispatches the dry run and reads its cost.
- Files to read first: `.github/workflows/bench-live-compile.yml`.
- Do not touch: `benchmark/decisions/harness-*.json` (frozen).
