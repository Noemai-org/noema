# Status · CON-08 · scorecard-view.v1 integer bounds (Core Engineer 1)

Updated 2026-11-20. noema#831 (A446, A298/A311, REQ-UI-004, AGENT-15). Tests first.

## Session 2026-11-20 · CON-08

## Session summary
- What changed (PR links): noema#831: every integer of `scorecard-view.v1` has a minimum of 0 and a maximum of at most 9007199254740991 (nine fields on `Latency`, `Slice` and `Tier0.severity_counts` named in noema#808, and six more the first scan missed because their type is `["integer","null"]`: `TurnsToResolution.median/p90/max`, `Tier3.cost_usd_micros_total` and `cost_usd_micros_per_correct_outcome`); five invalid fixtures; a test over every integer of the contract; generated models regenerated.
- Why: noema#808 (FE): `JSON.parse` loses integers above 2^53-1 before any parser sees them; the UI must show counts exactly (A446).
- Red first: `python3 tools/contracts/validate.py` listed the five fixtures as accepted; the new test listed the unbounded integers. One correction made after the RED commit, stated here: the test first demanded a maximum of exactly 2^53-1, which would have widened the pass-k `runs_supplied` (maximum 1000); it now demands a maximum that exists and is at most 2^53-1, and `runs_supplied` keeps 1000.
- Verification: `validate.py` passes, `unittest discover tools/contracts` and `tools/codegen` pass, `generate.py --check` passes, the UI build and `node --test` (282) pass, `go test ./internal/contracts/` passes, lint OK.
- Debt entries: none. Conflicts raised: none.

## Not done here (needs BO and the Architect)
A462 (committed): rates are integer basis points, spread is the min and max across runs, arm labels come from the arm manifest, the operating point is picked by the SoT §38.4 rule with a test. `scorecard-view.v1` still has floating `rate`, `wilson95`, `exact_upper95` and `mean`. Making them integers breaks every consumer: a new major `scorecard-view.v2` (AGENT-15). Not invented: the field names of the basis-point rates, the curve points and the chosen operating point, the spread member, and where the arm labels come from. Questions posted on noema#808.

## Pre-work for the next agent
- Exact next step: BO states the v2 field list (or approves the proposal on noema#808); C1 writes the schema, fixtures and generated models red first; FE regenerates the benchmark parser.
- Files to read first: `contracts/schemas/scorecard-view.v1.json`, `benchmark/runner`, A462.
- Do not touch: the v1 file's names; `migrations/README.md`.
