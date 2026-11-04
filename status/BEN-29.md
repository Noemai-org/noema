# Status · BEN-29 (Benchmark Owner agent) · integration suite as one command each

Updated 2026-11-03. Owner: the Founder (benchmark owner B). Needs: BEN-12, BEN-16, SEC-06, SEC-10 (none on `main`; checked 3 Nov, `origin/main` b2047e4). Gate: G3. REQ-BEN-004, REQ-TST-005.

## State

- **Plumbing built, ahead of the needs.** `benchmark/suites/`: suites as data (`suites.json`), pure selection, a scored subset, `python -m benchmark.suites list|run`, `suite-run.json` with the run ID, and `make bench-us|bench-chaos|bench-evil|bench-full` (each starts with `make reset`). The runner gains `--only-file`. Tests first (seen failing): `test_select.py`, `test_subset_and_run.py`, the runner's `OnlyFile` tests.
- **Why now.** The standing orders say to start when prerequisites are on `main`; the selection, recording and command do not depend on BEN-12, BEN-16, SEC-06 or SEC-10 (they supply cases and the chaos matrix, which join through tags, and the environment that lets the runner run).
- **Observed locally (3 Nov):** `us` selects 74 cases, `chaos` 149, `evil` 74, `full` 320 (seed 20261020); all four exit 3, record `NOT_RUN` and score nothing, because the runner has no environment. No case was run.
- **Not executed:** the `make bench-*` target itself (its `reset` step destroys the local Compose project and the authoring session's safety check blocked `make` here). The Makefile recipe is read and each step of it was run directly.

- **BEN-05 pre-staged (founder work order, 3 Nov):** `g1` suite (six cases covering US-01 or US-06) and `make bench-g1`: two legs, each after `make reset`, own run IDs, same commit, then the replay diff with the named open exclusion. Selection tested (`test_g1_is_every_case_covering_us_01_or_us_06_and_nothing_else`); the target itself is not executed here (it needs the environment and `benchmark/replay` from #102).

## Open

1. **Done-when ("one command runs each suite from `make reset`; run IDs recorded → G3")** is met for the command and the record, not for a run: a real run ID needs the environment (fixture loader, core, oracle counters), the Integration Lead's and Core Engineers' work. Unknown until then.
2. **Hidden-oracle and held-out parts** stay with the Founder (BEN-26, BEN-28); nothing here touches them.
3. **TST-06** (per-merge chaos subset, nightly full matrix) is next and reuses `chaos`; it needs TST-05 and SEC-10.

## Messages

### 2026-11-03 (time not recorded) accepted · Founder (standing orders, relayed by the user) → Benchmark Owner agent
- Purpose: start tasks when prerequisites are on `main`; no scope cut; decide inside scope; report in the stated order.
- Receiver used it for: building BEN-29's plumbing ahead of its date.
- Evidence: the PR for this branch.

### 2026-11-03 · Codex review of noema#117 (noemai-systems, on c188b9a), answered
- Finding (Major): the G1 recipe could swallow a failing suite exit code: each leg was allowed to return 1, then only the replay's status was returned, so identical partial runs could make `bench-g1` exit 0 even when a leg was incomplete or a Tier 0 gate was nonzero.
- Done: the orchestration moved out of shell into tested Python (`benchmark/suites/g1.py`, `python -m benchmark.suites g1`). `combine` keeps every failure: not-run (3) and bad input (2) win, then the replay's code, then 1 if any leg reported 1; a leg with exit 1 still lets the replay report be written. Eleven tests (`test_g1.py`) and an end-to-end NOT_RUN test; the Makefile target is now one call. The replay step also checks that `benchmark.replay` exists and exits 2 with a message when it does not (it arrives with #102), so a missing module is never read as a difference.

### 2026-11-04 · G1 option B: evidence labelled "stand-in"
- The Founder decided G1 runs on a Noema-side stand-in for the US-01 and US-06 data, evidence labelled "stand-in", rerun against live Tushe once its OpenAPI is frozen. `python -m benchmark.suites g1` now requires `--data-source stand-in|tushe` and writes `g1-evidence.json` (headline, `is_evidence`, `rerun_against_tushe_required`, both run IDs, commit, seed, open exclusions). `make bench-g1` defaults to `stand-in`. Seven tests first (seen failing).
