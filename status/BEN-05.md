# Status · BEN-05 (Benchmark Owner agent) · G1 evidence run, replay diff

Updated 2026-10-31. Owner: the Founder (benchmark owner B). Agent: Benchmark Owner role agent. Needs: OUT-01, REC-03. Gate: G1.

## State

- **Tooling built, run not done.** `benchmark/replay/` (`python -m benchmark.replay`) diffs two recorded runs of the same cases. It was written test-first (`test_compare.py`, `test_cli.py`, 36 tests; seen failing before `compare.py` and `__main__.py` existed). `make bench-test`: 545 tests and `mypy --strict` clean on this branch.
- **The run itself waits for OUT-01 and REC-03.** Neither is on `main` (checked 31 Oct, `origin/main` 34f6a8b): `services/core/outcome/**` holds boundary packages only and `status/EXE-04.md` records OUT-01 as not started. No run ID exists for BEN-05. Nothing here is G1 evidence.
- **Cases:** in the default suite (seed 20261020, generator 1.3.0, 320 cases, byte-identical on two generations) six cases cover US-01 or US-06: `BEN-CLEAN-0001`, `-0014`, `-0027` (US-01), `BEN-TMO-0001`, `-0018` (US-06), and `BEN-CLEAN-1007` (US-01 with `INJECTION_IN_NAME`). The runner can run the first five today (no unmet fault or event). `BEN-CLEAN-1007` needs `mock-customer INJECTION_PAYLOAD`, a Tushe-held fault.

## Decision (founders, 1 Nov 2026): compare the five runnable base cases

BEN-05's G1 evidence compares the five runnable base cases (`--exclude-mutated`, which removes only the cases in `benchmark/replay/open_exclusions.json`): `BEN-CLEAN-0001`, `-0014`, `-0027` (US-01) and `BEN-TMO-0001`, `-0018` (US-06). `BEN-CLEAN-1007` (US-01, `INJECTION_IN_NAME`) is a **named, open exclusion**, not a drop: it needs `mock-customer INJECTION_PAYLOAD`, a Tushe-held fault, and stays open until that fault is wired and the case can be run and compared. Every report states "covers 5 of 6 cases" in its selection row and lists the exclusion under "Open exclusions" (`open_exclusions` in `replay.json`). The case stays in the G2 and G3 runs. Nobody may describe the G1 evidence as covering all of US-01 and US-06.

## How BEN-05 runs the day OUT-01 and REC-03 land

1. Record the commit SHA (the same for both runs) and generate the suite: `python -m benchmark.generator generate --seed 20261020 --out S`.
2. Two separate runs, each with its own `--run-id` and `--commit-sha`, restricted with `--only` to the selected case IDs, from a clean environment (`make reset`). A rerun is a new run with its own ID; no run is repeated selectively.
3. `python -m benchmark.replay --suite S --first A --second B --story US-01 --story US-06 --exclude-mutated --out OUT`.
4. Report the verdict, n, seed, commit and both run IDs exactly as printed. DIFFERENT or NOT_COMPARABLE is reported, not retried.

## Limits (stated, not hidden)

- The diff covers the recorded case result, not the Trust decision payload or the Record events (the result does not hold them). If G1 needs the Record events compared, that needs a runner change (BEN-03 area) and a decision.
- Nothing was run against a live core, mock-case, Tushe or a model. No `bench-live` dispatch. Spend: none by this work.

## Messages

### 2026-10-31 (time not recorded) accepted · Founder (role prompt) → Benchmark Owner agent
- Purpose: assess BEN-01 to BEN-05 against `main`; prepare BEN-05 so it runs when OUT-01 and REC-03 land; then the G2 case families in tasks.csv order; DEC-2026-10-31 additions in the stated order.
- Receiver used it for: the assessment (`status/roles/benchmark-owner.md`) and this tooling.
- Evidence: this PR.

### 2026-11-02 · Architect decision on Codex's finding (noema#102)
- Purpose: `--exclude-mutated` may exclude only the named open exclusions (`BEN-CLEAN-1007`), never an errored or missing case, and an errored or missing story-covering case makes the overall verdict NOT_COMPARABLE. The Codex text itself was not visible on the PR (no review thread or comment from it); the change follows the Architect's wording.
- Receiver used it for: `compare(..., open_exclusions=...)` replaces the boolean; the registry `open_exclusions.json`; selection value `NAMED_OPEN_EXCLUSIONS_REMOVED`; 8 new tests (a second unnamed mutated case that errors or goes missing, a registry entry for an absent case, an exclusion that cannot hide a difference).
- Evidence: the commit that follows on noema#102.

### 2026-11-03 · Codex review of noema#102 (noemai-systems, 1 Nov 14:04Z, on 9cc90d9), answered
- Finding (Major): an excluded errored or missing case could still give an overall IDENTICAL. Codex's option 2: keep the base-only comparison but never let the top-level verdict hide an errored or missing excluded case.
- Done: the first fix (2 Nov) limited exclusion to the named registry. This change finishes it: every named case is probed; one that can be compared is counted (so it cannot hide a difference); one that cannot stays an open exclusion with `observed` (what each run showed), the report says "IDENTICAL on 5 of 6 cases (1 open exclusion, so not full coverage)", and `replay.json` carries `full_coverage: false`. An unnamed errored or missing case still makes the verdict NOT_COMPARABLE.
- Reading of the Architect's rule: `BEN-CLEAN-1007` is, today, an errored or missing case by construction (its fault is Tushe-held), so a rule that made every errored or missing case, named or not, force NOT_COMPARABLE would make the founders' 5-of-6 G1 evidence unreachable. The named exclusion is therefore reported as non-full coverage rather than as NOT_COMPARABLE. If the Architect wants NOT_COMPARABLE there too, it is one condition in `compare.py`.
