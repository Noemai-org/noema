# benchmark/publication

**Implements:** REQ-BEN-010 (task BEN-31); DEC-2026-10-31-proof-depth-and-completeness D28; SoT §38.8, G7. **Owning track:** T4 (benchmark owner, B, with the Founder). **Updated:** 2026-11-03.

The public benchmark format and the publication rules, with a check that enforces them. A number may leave the company only if `python -m benchmark.publication check` says "no problem" for the publication that carries it. Generated layer only: the held-out set is private (OD-16, INV-22) and this tool refuses any argument or environment value that names it.

## The format (what is public, what is not)

| Public | Private |
|---|---|
| The harness: generator (`benchmark/generator`), runner, oracle client, scoring, suites, publication check. Open source and reproducible from a seed | The held-out set (at least 100 cases, kept in `benchmark-heldout`, run by the benchmark owner only) |
| The public cases: generated cases, their fixtures and fault plans, in `benchmark-case.v1` shape, with the generator version and seed | The Tushe administrator and fixture credentials; any partner data (none exists in the POC) |
| The oracle documents of the public cases (`benchmark-oracle.v1`): the correct target, the true state, the unowned references | Provider keys, secrets, anything on the held-out side |
| The result, run-manifest and scorecard shapes (`benchmark-case-result`, `benchmark-run-manifest`, `benchmark-scorecard`) | |

**State-based outcomes.** A case is judged by the final state of the systems the case touched: the oracle client reads the synthetic enterprise's effects (what was really applied, how many times, to what) and compares them with the oracle. What the system or a model *said* never decides a pass. A claim must carry `outcome_basis: ORACLE_FINAL_STATE`.

**Consistency.** Understanding-dependent cases are reported as pass^k with k = 5 (all five runs correct); with any other number of runs the number is "not available" and cannot be published.

## The rules (each is checked, not advised)

| # | Rule | Check |
|---|---|---|
| R1 | Every external number traces to a run ID in the evidence bundle | A claim names `run_id` and a pointer into that run's scorecard; k, n, rate and interval must equal the scorecard's |
| R2 | Every number carries n, method, bound and confidence interval | `k`, `n`, `rate`, `ci95` (Wilson 95%), `method`, and the rule-of-three `bound` where the scorecard has one (zero failures) are required |
| R3 | Nothing is cherry-picked; the full distribution is shown, once | One record per run and slice (a duplicate is refused, never overwritten). Every family (B1 to B8 present) and every category of the scorecard appears, with matching counts, for each cited run |
| R4 | Tier 0 is published first, beside every other number | Each cited run needs a Tier 0 claim, **listed before any other claim of that run**; each claim states `tier0_all_zero` and it must equal the scorecard's |
| R5 | No text outruns its evidence | Every number written in a claim's text must be that claim's own k, n, rate, interval or bound; a signed (-99%), ranged (5-99%) or suffixed (99th, 1.5x) number is a number too; identifiers such as B4 and US-01 are not numbers |
| R6 | A run that is not complete is not published; one that is not a release result says so; the population is the scorecard's | `complete` must be true; `population` must equal the scorecard-derived population exactly (`population_for`), which carries "not a release result" for a non-release run |
| R7 | Outcomes are judged against the final state of the systems | `outcome_basis` is `ORACLE_FINAL_STATE` |
| R8 | pass^k only when it was measured | A pass^k claim requires `available` (exactly five runs) in the scorecard |
| R11 | None of the words the release rules forbid (tamper-proof, immutable, compliant, certified, guaranteed, zero risk, proven in production); the Record is tamper-evident | The words are refused in a claim's text (`docs/poc/execution/release/claims.md` section 3). Other prose stays a review item |
| R9 | Demos run from the engine, never scripted; voice is shown live only if G5 passes | Not machine-checkable here: the G7 dry run (DEM-02) and the G5 record decide. Listed so the checklist names them |
| R10 | No external number without a source in the SoT, no ROI claim, no regulatory-compliance claim | Review item at G7 (AGENT-18, SoT §50.2); the check cannot judge prose |

## Use

```
python -m benchmark.publication claim --scorecard OUT/score/scorecard.json --id C1 --ref /tier1/correct_terminal_handling --text "Correct terminal handling" > claim.json
python -m benchmark.publication check publication.json --scorecards EVIDENCE_BUNDLE/scorecards
```

`claim` copies the numbers from the scorecard, so an author never types one. A publication is `{"publication_id", "claims": [...], "distributions": [{"run_id", "slice": "by_family"|"by_category", "rows": {KEY: {"n", "k"}}}]}`. Exit 0 no problem; 1 problems (all listed, sorted); 2 unreadable input.

## What it does not do

It cannot judge prose, a demo or a chart. R9 and R10 stay with the founders at G7. A passing check says the numbers are the scorecard's and complete; it does not say the claim is wise.

## Tests

`make bench-test`: `test_check.py` (31 tests: a built publication passes; each rule has a case that breaks it and is named in the problem).
