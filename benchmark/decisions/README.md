# benchmark/decisions

Dated decision records for the benchmark's measurement rules: what is measured, with which pins and gates, before a run. A record here is operational (how a benchmark run is done); it does not define what Noema is, and a decision about the product belongs in the spec first (AGENTS.md §2).

## What is built

- `2026-10-27-model-selection.md`: the freeze of the initial OpenAI model comparison (`gpt-6-luna`, `gpt-6.1-terra`): candidates and sources, pins, rate card, corpus, prompt-template and scoring-code hashes, retry policy, the seven gates and how they apply to the corpus, the decision rule, the budget and its guard, key handling, the pilot and the full run, and the decisions D1 to D9 made where the instruction was silent. Its ```` ```json frozen ```` block is machine-read: `benchmark/bakeoff/test_freeze.py` recomputes the corpus and scoring-code hashes and `services/model/tests/test_freeze_record.py` checks the template digest, on every CI run.

## Constraints

- A record is frozen when merged. Changing a frozen value (the corpus, the scoring code, a gate, the retry policy, the template) needs a new dated record; the tests above fail otherwise.
- No live API call is made before the freeze record is merged. A live run needs the founder's approval through the `bench-live` environment.
- Synthetic data only; nothing held out is read (INV-22).

## Open

- The identifier the API returns for each model is unknown until the pilot.
- The pilot's measured cost and the full-corpus estimate are posted on the freeze PR after the pilot.
- The Responses adapter is its own pull request; the live decider, the budget guard and the `bench-live` workflow are another. Neither is part of the freeze.

## Files

- `harness-gpt-6-luna.json`, `harness-gpt-6.1-terra.json`: the harness configuration each run reads (model ID, endpoint, the key's variable name, `reasoning.effort`, `max_output_tokens`, timeout, rate card, routes). Frozen by SHA-256 in the record. The key itself is never in them.
- The live code is `benchmark/bakeoff/{budget,live,live_run}.py`, started only by `.github/workflows/bench-live.yml` (`environment: bench-live`, a required reviewer).

