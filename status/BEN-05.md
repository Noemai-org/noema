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

# Session 2026-11-14 · BEN-05 · the G1 evidence package template

## Session summary
- What changed: `benchmark/suites/G1-EVIDENCE-PACKAGE.md` (the Security Reviewer's twelve criteria as a table: what closes each item, who produces it, how BO checks it; the dispatch preconditions; the package outline) and a pointer in `benchmark/suites/README.md`.
- Why: the Founder's instruction of 14 Nov 2026: prepare the G1 package with SR's list; post READY TO DISPATCH when the stack, Trust and write lanes merge; the Founder dispatches.
- Risks, and how they are contained: documentation only. It holds no result and claims nothing is met. The criteria are SR's (noema#436); where the file and that comment differ, the comment is right.
- Tests added: none (docs). Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: when the preconditions in section 3 hold at one head, post READY TO DISPATCH at that SHA on noema#436 and noema#117; after the run, write the package from links opened.
- Open questions (owner): SR items 6, 8 and 9 (FaultPlan record, leak scan with a planted canary, negative probes) were not found in `bench-g1-stack.yml` at noema#444 `9f3e2ce8` (IL to confirm or add).
- Files to read first: `benchmark/suites/G1-EVIDENCE-PACKAGE.md`.
- Do not touch: the gate conditions; they are the gate record's.
- 2026-11-16: the template is updated after the Security Reviewer's three requests (prose not code-enforced; an explicit `--- PASS` line per property test; item 11 names every excluded case), EL's corrections (item 9 follows ruling A220: wrong role 404, foreign tenant 401, and a 401 is not the isolation proof; noema#601 blocks mounting `POST /v1/conversations`; the worker registration is the composition PR, not noema#576; the Reconciler is not built) and Voice's note (the compiler is noema#598; #581, #582 and #584 are merged).

# Session 2026-11-18 · BEN-05 · the package skeleton is built; READY TO DISPATCH is still not posted

## Session summary
- What changed: `benchmark/suites/g1_package.py` and 11 tests. `python -m benchmark.suites.g1_package --inputs F --a-score F --b-score F --replay F --out D` renders the section 4 package (twelve rows, the data label, the excluded cases, what was not tested) from the run's own files, so it is written in minutes after the run.
- Rules: a row is MET only with an https link the Benchmark Owner opened and says they read; row 5 (one final head: the dispatch SHA is a full SHA, the check runs and the run were made at it, every check run green), row 10 (both legs complete, Tier 0 at zero) and row 11 (replay IDENTICAL, the unknown-outcome case not excluded) are computed and no hand-written verdict overrides them; an empty "what was not tested" fails the package; the headline keeps STAND-IN data, not Tushe. Exit 0 only when every row is MET.
- Tests: 11 written first and seen failing; 13 mutations each caught (the first run left the SHA-shape check alive; a test now covers it).
- Preconditions of noema#566 section 3 on main at 18 Nov: 1 met (#444, #577, #591); 7 met (#563, #568); 8 met (#591, #577); 2 open (#572 merged, the Trust.Decide adapter #710 is open); 3 open (RunWrite #576 merged; the worker registration #676 is open); 4, 5 and 6 and 9 not verified as met by me (the compile-acceptance run, noema#546, has not run; G0 not confirmed). READY TO DISPATCH is not posted.
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: when all nine hold at one SHA, post READY TO DISPATCH; after the run, fill `package-inputs.json` (links and the read attestations), run the command, and ask the Security Reviewer to read the result.
- Open questions (owner): none new.
- Files to read first: `benchmark/suites/g1_package.py`, `benchmark/suites/G1-EVIDENCE-PACKAGE.md`.
- Do not touch: the gate conditions.

# Session 2026-11-18 (second round) · BEN-05 · Codex P2s on the package at 088635bc

## Session summary
- What changed: `g1_package.py` row 11 now needs every configured unknown-outcome case to be in the replay's `cases` with result IDENTICAL (a replay limited to US-01 no longer passes; a missing or unreadable case list is NOT MET); row 10 binds scorecard a to replay leg `first` and scorecard b to `second` by `run_id` (a scorecard of another run, a swapped pair, a missing, empty or non-string id is NOT MET), for a clean leg and for a leg accepted through a named exclusion.
- Tests: 6 new (written first, 4 failing before the fix); 12 mutations of the new guards each caught (two survived at first; a test now covers each).
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: after Codex re-reads the fix lines (A287), the independent read, then one Founder approval.
- Do not touch: the gate conditions.

# Session 2026-11-19 · BEN-05 · A355 (rows 5, 8, 9 computed from artifacts) and Codex's two P2s at 6c42b563

## Session summary
- What changed: `g1_package.py` now computes rows 5, 8 and 9 from the run's artifacts (A355): row 5 from the check-runs JSON (every run at the dispatch SHA, completed and green, `required_checks` present and successful, pages that add up to the total, a full SHA, the run made at it); row 8 from the leak-scan output (the probe line naming all six planted kinds, a `leakscan OK` line that read at least one file and one line, no failure line); row 9 from `negative-probes.json` (control 200, wrong role 404 NOT_FOUND_OR_NOT_AUTHORIZED, foreign tenant 401 SESSION_INVALID, the unknown correlation answered exactly as the wrong role is, for every expected case once, recomputed from status and code and never from an `ok` flag). Each computed row shows the sha256 of its artifact. The row-5 attestation of A322 is superseded by A355.
- Codex P2s: a case listed twice in the replay is NOT MET (a later IDENTICAL no longer overwrites an earlier failure); row 11 needs every case in `inputs.g1_cases` (both stories) IDENTICAL once each unless it is a named exclusion, and the unknown-outcome cases are never excludable.
- New inputs: `g1_cases`, `required_checks`. New CLI flags: `--check-runs`, `--leak-scan`, `--probes`.
- Tests: 30 new or rewritten, written first (the suite was red with the new API missing); 23 mutations of the new logic each caught (one survived at first, a green conclusion on a run that is not completed; a test now covers it).
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: after Codex re-reads the fix lines (A287), the independent read, then one Founder approval. After the run: fill `package-inputs.json` (links and read attestations for rows 1 to 4, 6, 7, 12; `g1_cases` from the g1 suite selection; `required_checks`), save `gh api --paginate repos/Noemai-org/noema/commits/<sha>/check-runs` as the check-runs file, then run the command.
- Do not touch: the gate conditions.

# Session 2026-11-19 (second) · BEN-05 · A375: row 2 computed from the Record

## Session summary
- What changed: package row 2 is now computed (A375, the Architect's A1), with no attestation. From the reconstructions: US-06 needs exactly one GRANT_SPENT, one DISPATCH_ATTEMPTED and one RECEIPT, the receipt classified OUTCOME_UNKNOWN (exactly one ACTION item), at least one VERIFICATION_READ (the read by key) and exactly one VERDICT whose verdict is ACHIEVED, in the order spend, dispatch, receipt, read, verdict, and the same event counts initially, after a recovery sweep and after a replay; plus exactly one write for the case in the mock-case write counts. US-01 needs ALLOW with no GRANT_* event, no DISPATCH_ATTEMPTED and zero writes; a grant on US-01 fails the row.
- New inputs: `us01_cases` (inside `g1_cases`, apart from `us06_cases`). New artifacts and CLI flags: `--reconstructions` ({case: {initial, after_recovery, after_replay}} of reconstruction.v3) and `--mock-case-writes` ({case: integer}). The format of the mock-case log is not known to me, so the extractor behind `mock-case-writes.json` is IL's or mine once IL gives the format.
- Tests: 14 new, written first (11 red before the code); 15 mutations of the row-2 logic each caught (three survived at first because the per-phase checks masked them; a test now isolates each).
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: IL names the mock-case log format and the runner's per-request record (asked on 19 Nov); then the extractor and the runner writer are added with tests.
- Do not touch: the gate conditions.

