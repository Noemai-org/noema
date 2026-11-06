# Model qualification (UND-13)

REQ-INT-006 (model-drift canary; DEC-2026-10-31 D25, AGENT-28): every model and speech provider version is pinned, and a version or provider change is used only after the
qualification subset, including pass^k, has run and the result is recorded. This module is the benchmark's half: the record format, how a record is made from a scorecard,
and the lookup that refuses an unqualified ID. The model service's configuration check (C2) is the other half and is not built here.

## What a record says

`qualification-record.v1`, one JSON file per run, made by `record` from a scorecard (`python -m benchmark.scoring ... --repeat-results` x4, so pass^k is available):

| Field | Meaning |
|---|---|
| `kind` | `llm` or `speech` |
| `provider`, `model_id`, `version` | exactly what the harness will be configured with. `version` must be pinned: an alias such as `latest` or `default` is refused |
| `run_id`, `n_cases`, `complete`, `tier0_all_zero`, `pass_k` | copied from the scorecard; nobody types a number |
| `scorecard_sha256` | digest of the scorecard, so an edited scorecard or record no longer verifies |
| `decision_record` | the decision record that holds the qualification (D25) |
| `qualified_on` | ISO date; the latest record for a provider, model ID and version decides |
| `verdict`, `reasons` | `QUALIFIED` only if the run is complete, Tier 0 is at zero and pass^k was available |

The verdict uses only those three conditions. The quality bar beyond them (for example a minimum pass^k) is a founder decision and is not set here (REQ-AGT-005).

## Commands

```
python -m benchmark.qualification record --scorecard OUT/score/scorecard.json --kind llm --provider P --model-id M --version V --decision-record DEC-... --qualified-on 2026-11-06 --out benchmark/qualification/records/P__M__V__2026-11-06.json
python -m benchmark.qualification check --records benchmark/qualification/records --provider P --model-id M --version V [--kind llm]
```

`record` exits 0 for QUALIFIED, 1 for NOT_QUALIFIED (the record is still written, so a failed qualification is on file), 2 for bad input. `check` exits 0 allowed, 1 refused, 2 unreadable input.

## The lookup contract for the model service (C2)

An ID may be used only if a valid `QUALIFIED` record matches its provider, model ID and version exactly, and the latest record for it (by `qualified_on`) is `QUALIFIED`. A later `NOT_QUALIFIED`
revokes; two records of one day that disagree refuse; an unpinned version is refused before any lookup. `record.check_model` is the reference implementation.

## Tests

`python -m unittest benchmark.qualification.test_qualification`. SYNTHETIC scorecards from the real scorer; generated layer only (INV-22).
