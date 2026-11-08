# Status · AGT-01 · Task-card and PR templates

Updated 2026-11-08. Owner: EL+AG in tasks.csv; executed by the Engineering Lead role agent. Task: task-card and PR templates in both code repositories: trace IDs, module allowlist, tests-first list, faults to exercise and the CONFLICT block (REQ-AGT-003, REQ-AGT-002).

## State
- In progress (Draft). The `noema` pull request template now follows the order of AGENTS section 11 (IDs, plan, SoT section 52 questions with the unsafe-autonomy question, checklist, hand-off report, CONFLICT block); an unfilled template fails the trace check on all three IDs (its own examples used to satisfy two of them). Copies of the task-card and conflict-block templates are in `docs/templates/`, each saying which original wins.
- Tests: `tools/ci/test_pr_template.py` (6), seen failing first.
- **Not met:** the task's "Done when" is behavioural: a trial agent task that hits a planted conflict stops and files the block instead of guessing. That needs an agent trial, which this PR does not run. AGT-01 stays not Done until the trial is run and recorded.
- The `docs` repository's templates already exist (`docs/poc/perd/templates/`); nothing is added there.

## Pre-work for the next agent
- Run the trial: plant a conflict between two cited sources in a throwaway task card, give it to an agent, and record whether it stops and emits the block.
- Re-point the copies in `docs/templates/` when the docs originals change.
