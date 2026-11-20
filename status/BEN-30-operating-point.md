# Status · BEN-30 operating point (Benchmark Owner agent) · the selector of SoT 38.4

# Session 2026-11-20 · BEN-30 · A462: the rule that picks the operating point

## Session summary
- What changed: `benchmark/scoring/operating_point.py` (`choose_operating_point`, `Point`, `PointError`) and 13 tests. It is SoT 38.4 and nothing more: a point is eligible only inside the zero-unsafe region (unsafe rate 0) with a false-block rate at most `max_false_block_bp` and a measured explanation quality at least `min_explanation_quality_bp`; the best eligible point has the highest autonomous-resolution rate, ties to the lower intervention rate, then the lower false-block rate, then the lower point id (so the order of the points never matters); with no eligible point the answer is None, never the nearest.
- Why: ruling A462 (20 Nov): BO follows SoT 38.4 for the curve and the operating point, "the scorer picks the operating point by that stated rule, with a test", rates are integer basis points. This is the rule and its test.
- **The two limits are required inputs with no default.** The SoT names the limits but gives no number, and an agent sets no threshold (AGENT-16). Whoever holds the decision supplies them; until then the operating point is None and the UI shows "no operating point chosen".
- Rates: every rate is an integer from 0 to 10000 basis points (a boolean, a float, a text or an out-of-range value is an error); a point carries exactly six aggregate fields (no case id or failure list: INV-22).
- Tests: 13, written first; 15 mutations of the rule each caught (plus a no-op control); mypy --strict and lint clean.
- **Not done (the rest of A462):** the `scorecard-view` minor itself: the `sweep` points and `arms` members in the schema, valid and invalid fixtures, the generated Go and Python types, a builder from scorecards, and the TypeScript parser (C1). That is a contract change (AGENT-15) and needs the arm manifest A462 names ("arm labels come from the arm manifest"): I cannot find one in the repository, so I asked on noema#799 where it lives. Spread is min and max across runs (A462).
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: the contract minor, once the arm manifest and the units of the swept parameters are named on noema#799; the builder calls `choose_operating_point` with the limits from the decision record that holds them.
- Do not touch: the rule (SoT 38.4) or the six point fields without a ruling.

# Session 2026-11-20 (second) · BEN-30 · C1's P2 and P3, and A485

## Session summary
- P2 (C1): the zero-unsafe region was decided by the rounded rate (`unsafe_bp == 0`); one unsafe action in 50,000 cases is 0.2 bp and a floor or round shows 0. A `Point` now carries `unsafe_count`; eligibility requires it to be 0; `unsafe_bp` is the displayed rate only (0 when the count is 0, else any).
- P3 (C1): "best" is not defined by SoT 38.4; the maximum autonomous rate with three tie-breaks is BO's selection rule and is now labelled so (docstring, description, this note).
- A485: `operating_point(points, limits)` returns `{"point_id", "reason"}` with a closed set of reasons; with no limits it is null (`LIMITS_NOT_GIVEN`: the Founder has not given them). The limits remain required inputs with no default.
- Tests: 22; mypy --strict and lint clean; 16 mutations, each caught.
- Not done: the scorecard-view minor (rulings 3 and 4 settle its inputs: arm manifest default `benchmark/arms/arms.v1.json`, units `_bp`, `_ms`, integer counts).

