# benchmark/nightly

**Implements:** TST-02 (full suite nightly), REQ-TST-005, REQ-MEM-002 (a failure becomes a regression case). **Owning track:** T4. **Updated:** 2026-10-21.

The nightly job (`.github/workflows/bench-nightly.yml`) runs `make bench`, then `python -m benchmark.nightly` files **one issue per failing case**, de-duplicated by case ID.

| Rule | Why |
|---|---|
| Only a case that was **run** and not correctly handled is filed | A case that could not run (status `ERROR`) is a gap in the environment, listed by code in the run manifest. Filing 195 of them nightly would bury the real failures |
| A case with no recorded result is not filed | Nothing to reproduce |
| A case with an **open** issue is not filed again | The case ID in the body's marker `<!-- bench-case: BEN-... -->`, or in the title, identifies it |
| A **closed** issue does not count | A case that fails again after a fix is filed anew |
| Most severe first; Tier 0 failures carry the `tier-0` label | Severity from the case (`severity:S0` to `severity:S3`) |
| A run that was `NOT_RUN` files nothing and says why | Nothing was run, scored or faked |

No cap on the number filed: a systemic regression is one issue per case, as asked.

```
python -m benchmark.nightly --scorecard OUT/score/scorecard.json --results OUT/run/results --run-manifest OUT/run/run-manifest.json --run-url URL [--dry-run]
```

Needs `GH_TOKEN` (the `gh` CLI) unless `--dry-run`; the workflow uses its own token and reads no secret. Filing a labelled issue needs `issues: write`, which the workflow declares.

## What the workflow does today

Exit codes of `make bench`: 0 complete and Tier 0 at zero; 1 incomplete or a gate not at zero; 2 bad input; **3 not run, no access**. The job passes on 0 and 3 and fails otherwise. Until a core and a fixture loader exist every night is exit 3, and the job summary says so with the reason from the run manifest.

## Gaps

- The runner has no Compose service yet (topology decision, see `benchmark/runner/README.md`), so the nightly runs the runner from the host and reaches only what the job can reach.
- No artefact upload: the scorecard and report go to the job summary. Adding one needs a pinned `upload-artifact` and a founder's decision on retention (the results are synthetic, but they hold case text).

## Tests

`make bench-test`. `FakeGh` replaces the `gh` CLI in the tests; the real wrapper is tested for the arguments it builds.
