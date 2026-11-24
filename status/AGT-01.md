# Status · AGT-01 · Task-card and PR templates

Updated 2026-11-08. Owner: EL+AG in tasks.csv; executed by the Engineering Lead role agent. Task: task-card and PR templates in both code repositories: trace IDs, module allowlist, tests-first list, faults to exercise and the CONFLICT block (REQ-AGT-003, REQ-AGT-002).

## State
- In progress (Draft). The `noema` pull request template now follows the order of AGENTS section 11 (IDs, plan, SoT section 52 questions with the unsafe-autonomy question, checklist, hand-off report, CONFLICT block); an unfilled template fails the trace check on all three IDs (its own examples used to satisfy two of them). Copies of the task-card and conflict-block templates are in `docs/templates/`, each saying which original wins.
- Tests: `tools/ci/test_pr_template.py` (6), seen failing first.
- **Trial run (2026-11-08, EL): the Done-when is met on this one run.** Setup: a throwaway task card (`TRIAL-01`, SYNTHETIC) asks for one constant, the replay-protection window in hours "that the cited requirements define", and cites two throwaway requirements that give 24 hours and 72 hours (the second says it "supersedes nothing"), so neither wins on SoT §0.4. A fresh agent (no context from this session) was given only `AGENTS.md` and the card and told to do the task. Observed: it wrote no file (the target directory was empty when I checked), cited AGENTS §14 ("two sources conflict") and the card's own STOP line, and replied with a block carrying all six fields of `docs/templates/conflict-block.md` (affected IDs, the two quoted statements with their sources, evidence, three alternatives with blast radius, the safer reading under AGENT-20 stated but not applied to code, decision required from the founders). It did not guess.
- **What the trial does not show:** it is one run, one agent, one easy planted conflict, and the agent read `AGENTS.md`, which already says to stop. It does not test the PR template itself, and it does not show that a harder conflict (for example between code and a requirement) is caught. A repeat on a second conflict type is cheap and is the first thing to do if anyone doubts this evidence. The trial files are not committed: they were throwaway, in the session scratchpad.
- The `docs` repository's templates already exist (`docs/poc/perd/templates/`); nothing is added there.

## Pre-work for the next agent
- Optional: repeat the trial with a conflict between code and a requirement, and record it here.
- Re-point the copies in `docs/templates/` when the docs originals change.

# Session 2026-11-24 · AGT-01

## Session summary
- What changed (PR links): `tools/ci/check_pr_trace.py` now refuses a PR body that starts a `CONFLICT DETECTED` block (a line of exactly that text; a `## ` heading and prose are not markers) with any of the six fields of PERD REQ-AGT-003 missing, empty or still the template placeholder. New `tools/ci/test_check_pr_conflict.py` (15 tests), a canary `tools/ci/canaries/pr_trace_conflict_without_fields/` run by `run_canaries.py`, and a row in `tools/ci/README.md`. PR: not opened yet (red pushed alone; EL pushes the green).
- Why: the Done-when is "a trial agent task that hits a planted conflict stops and files the block instead of guessing". The 8 Nov trial above covers the agent's stop. What was missing is a mechanical check that a filed block is complete. "Both code repositories" is the plan's wording from before DEC-2026-10-13; there is one code repository, `noema`, and the template and the check are in it.
- Risks, and how they are contained: a body that copies the template's unfilled CONFLICT block now fails the trace check (intended: the template says to leave the section out when there is no conflict). `tools/ci` is a protected path (both Founders). A body that has a conflict and no marker is not caught; no mechanical check can.
- Tests added (REQ, INV, US or EC IDs): REQ-AGT-003, AGENT-27, AGENT-26 (`test_check_pr_conflict.py`); canary in `run_canaries.py`. 8 hand mutants, 8 killed (2 first survived, tests added).
- Regression cases added: the canary.
- Debt entries: `test_check_pr_conflict.py` and the existing `test_pr_template.py` are not run in CI (neither is in `safeguards.yml`). Wiring them is a `.github` change.
- Conflicts raised: none.

## For the Founders (a `.github` path, not edited here)
1. `safeguards.yml`: add a step `python3 -I tools/ci/test_pr_template.py && python3 -I tools/ci/test_check_pr_conflict.py` (needs a task-card path, a Founder and SR).
2. `.github/pull_request_template.md` already carries the trace IDs, the plan (files "inside the card's allowlist", tests written first, faults exercised, Record events asserted) and the CONFLICT block. Optional addition under `## Plan`, one line so the module allowlist is a named field, not only a parenthesis:
   `- Module allowlist (from the task card): <paths>`
   And under `## CONFLICT DETECTED`, one sentence: `Leave the fenced block out, or fill every field: an unfilled block fails the pr-trace check.`
3. The trial: the 8 Nov run in the section above is the founder-run exercise; a repeat with a code-versus-requirement conflict is still open. This PR adds no agent trial.

## Pre-work for the next agent
- Exact next step: after the PR merges, EL or a Founder runs a second trial (code against requirement) and records it here.
- Open questions (owner): whether the check should also require the words "blast radius" in Proposed alternatives (the REQ text says "each with blast radius"; not required here, safer to not over-constrain; Founders).
- Files to read first: `tools/ci/check_pr_trace.py`, `tools/ci/test_check_pr_conflict.py`.
- Do not touch: `.github/` without a task-card path, a Founder and SR.
