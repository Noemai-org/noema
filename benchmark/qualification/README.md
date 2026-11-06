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

## The drift canary: subset, baseline, run record (what C2 builds to)

Two documents answer two questions. The **qualification record** above says "may this version be used" (a verdict from a scorecard with pass^k). The **run record** below is what the model service's
canary emits: "does this version still answer the way the pinned baseline did". C2 builds the canary in `services/model`; this module owns the subset, the baseline and the format.

| Document | File format | Made by |
|---|---|---|
| Subset | `qualification-subset.v1`: the GENERATED, understanding-dependent cases (full case documents), sorted, with `cases_sha256` | `python -m benchmark.qualification subset --cases CASES.json --subset-id ID --out subset.json` |
| Baseline | `qualification-baseline.v1`: for each subset case, the slots the previously qualified, pinned version answered, plus the commit SHA | `... baseline --subset subset.json --answers answers.json --kind llm --provider P --model-id M --version V --commit-sha SHA --out baseline.json` |
| Run record | `qualification-run.v1`, JSON Schema in [`schemas/qualification-run.v1.schema.json`](schemas/qualification-run.v1.schema.json) | `... compare --subset ... --baseline ... --answers new-answers.json --kind ... --version NEW --commit-sha SHA --recorded-on DATE --out run.json` |

An answers file maps each subset `case_id` to `{"slots": {...}, "answered_by": "<version the provider reported>"}`, or to `null` when the provider did not answer.

**The run record** (the canary emits exactly this; `run.compare` is the reference implementation, `run.validate_run_record` the validator):

- `pinned`: `kind`, `provider`, `model_id`, `version` the service was configured with. The version must be pinned (dated or numbered), never `latest`.
- `baseline`: the pinned version, commit SHA and digest of the baseline compared against. The baseline must be for the same kind, provider and model ID (the version is what changes).
- `cases[]`: per case `status` (`MATCH`, `DRIFT`, `UNAVAILABLE`), `answered_by` (the version that answered), `version_mismatch`, `differing_slots` (`slot`, `expected`, `observed`; `expected` is absent for an extra slot and `observed` for a missing one), and the digests of the two answers.
- `result`: `DRIFT` if any case drifted (a different slot value, a missing or extra slot, or a version that answered under another version than the one asked for, even when every slot matches); else `PROVIDER_UNAVAILABLE` if any case got no answer; else `PASS`. Drift outranks unavailable.
- `commit_sha`, `recorded_on`, `answered_versions`, and `layer` (always `GENERATED`).

**Boundaries.** The subset holds GENERATED cases only; `select_subset` refuses a held-out or partner case anywhere in its input (INV-22), so none of it reaches the model service. The baseline is a recorded model answer, never the oracle's expectation. The canary
must send the subset through the same provider adapters the service uses; this module never calls a model.

**What a run record does not decide.** Whether a drift is acceptable, and whether the new version is then qualified, is the qualification record's decision record (a founder decision). A `PASS` here is not a qualification.
