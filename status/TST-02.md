# Status · TST-02 (Benchmark Owner agent with EL) · the 30-case smoke suite

# Session 2026-11-17 · TST-02 · the smoke suite is defined by a rule

## Session summary
- What changed: `greedy_cover` in `benchmark/suites/select.py`, the `smoke` suite in `suites.json`, 12 tests in `test_select.py`, a README section.
- Why: the plan row says "30-case smoke benchmark on every merge; full suite nightly". The smoke is the fewest cases that touch the most identifiers, chosen by a deterministic rule (first every user story, then fault effects, then capabilities and edge cases), not a list typed by hand that would rot.
- Result at seed 20261020: 30 cases covering 22 of 22 user stories, 24 of 28 fault effects, 16 of 20 capabilities, 25 of 105 edge cases and all 10 categories; digest `sha256:2a2e2bbf1d752155fe6d4e32be333582c313a85703c4ab5adff196355e642986`. It is a smoke, not coverage; `full` and `chaos` cover the rest.
- Not done, on purpose: "on every merge" needs the stack up in CI and a workflow change (Founder approval and SR), and "nightly" is `bench-nightly.yml`'s. The existing `make bench-smoke` is a different, older target and is untouched.
- Risks, and how they are contained: a new selector key refused in combination with any other, with every malformed rule an error, never a default. The existing four suites are unchanged (their tests pass).
- Tests added: `GreedyCover` (9) and `SmokeSuite` (4), seen failing first; seven mutations of the selector each caught (the first run left a dead "fill" stage alive, which I removed because it could never change a result).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Founder and SR decide the CI wiring (a job that brings the stack up and runs `smoke` on merge); until the stack runs in CI, `smoke` is run by hand.
- Open questions (owner): whether 24 of 28 fault effects is enough for a smoke (EL, the Founders).
- Files to read first: `benchmark/suites/select.py`, `benchmark/suites/README.md`.
- Do not touch: `suites.json` entries other than `smoke`.
