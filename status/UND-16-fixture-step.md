# UND-16 · the CI step that proves the G1 replay fixture equals fresh output

State: PR open (A478). Workflow change only; SR approves the step, a non-author Founder approves the `.github` change.

# Session 2026-11-20 · UND-16 (fixture equality step)

## Session summary
- What changed: `.github/workflows/core.yml` gains one step in the `tests` job (shard 0 only) and one path in the `changed` filter (`benchmark/replay/g1_`, so a change of BO's inputs or of the fixture alone runs the core job). The step runs the Go generator of noema#811 (`TestWriteG1Fixture`) with the two paths on its command line, writes the fresh output to a temp file and compares it byte for byte with the committed fixture. No lint exemption (A478).
- Why: A460 asks CI to prove the fixture equals fresh output; A478: "CI proves the fixture equals fresh output through a step in the core tests job. SR approves the step and you approve the .github change. No lint exemption."
- Risks, and how they are contained: inert while the generator is not on the tree (it prints one line and passes); once it is, a missing inputs file, a missing fixture, a skipped generator, a generator that does not PASS, an empty output or any difference fails the job. The tree is never written. No new action, tool or dependency.
- Tests added: none in Go (a workflow step); proved by a local reproduction of the same commands (see the PR body).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: when noema#811 (the generator) and the real fixture `benchmark/replay/g1_canonical_inputs.json` land, the step starts proving. If the fixture is absent when the generator lands, the step fails (closed) until it is committed.
- Open questions (SR): the step read. Files to read first: `.github/workflows/core.yml` (step "G1 replay fixture equals fresh generator output"). Do not touch: CODEOWNERS, the generator's flags (`-g1-inputs`, `-g1-fixture`).

## Session 2026-11-21 · UND-16 · SR's NOT READY on the inert branch (C2)
- **What changed:** the step skips only when NEITHER the generator nor the fixture is on the tree. A fixture with no generator (a renamed, moved or deleted generator) now fails with a fixed message (`exit 1`). SR's finding at `e9ebe8d2`: the old `if [ ! -f generator ] ... exit 0` passed whenever the generator was absent, so the fixture could stay on main unproven, the opposite of A460.
- **Observed (the step's `run:` extracted from the YAML, run with its `env`, three trees):** neither file: passes, one line; fixture and no generator: `exit 1`, "the replay fixture is on this tree and its generator is not: nothing can prove it equals fresh output"; generator and no fixture: `exit 1`. The generator-present cases (equal, one byte different, stale inputs) are as in the earlier section.
- **CI on this PR** still exercises only the "neither" branch (#811 is not on main); that is stated, not hidden.
- Debt: none. Conflicts raised: none.
