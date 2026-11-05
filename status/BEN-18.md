# Status · BEN-18 (Benchmark Owner agent) · replay the full suite twice

Updated 2026-11-04. Owner: B. Needs: BEN-17b (not started). Gate: G3. REQ-NFR-001, REQ-NFR-004.

## State
- **Built:** `python -m benchmark.replay --all`: the replay diff over every case of the suite (not two stories), same rules (an errored or missing case is NOT_COMPARABLE; a named open exclusion is non-full coverage). Tests first (seen failing), six new tests, the report wording for the whole-suite mode.
- **Why this is BEN-18's check:** the compared fields are everything Tier 0 and Tier 1 are computed from, so IDENTICAL on every case means identical Tier 0 and Tier 1.
- **Not done:** the two real `full` runs. Today 177 of 320 generated cases cannot run (unwired faults and events), so the check cannot pass yet and the report will say NOT_COMPARABLE, not hide it. The one-command orchestration (two `full` legs from `make reset`, then `replay --all`, labelled with its data source) reuses `benchmark/suites/g1.py` and lands after noema#117 merges.

## Open
1. Orchestration after #117: generalise `run_g1` to a suite pair (`full`) and add the `--all` replay.
2. BEN-17b (fix the failing cases) is C1/C2's; BEN-18 can only be green after it.
