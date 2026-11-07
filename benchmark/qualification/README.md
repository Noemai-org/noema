# Model qualification (UND-13)

REQ-INT-006 (model-drift canary; DEC-2026-10-31 D25, AGENT-28; ruling A17 of 6 Nov 2026): every model and speech provider version is pinned, and a version or provider change is used only after the
qualification subset, including pass^k, has run, the drift check has passed and the result is recorded. This module is the benchmark's half: the formats, how a record is made, and the lookup that
refuses an unqualified ID. The model service's drift canary and its configuration check (C2) are the other half and are not built here.

## The two contracts

| Contract | File | Meaning |
|---|---|---|
| Run record | [`contracts/schemas/qualification-run.v1.json`](../../contracts/schemas/qualification-run.v1.json) | What the canary emits: a new pinned version's structured answers to the subset against the pinned baseline. `PASS`, `DRIFT` or `PROVIDER_UNAVAILABLE` |
| Qualification record | [`contracts/schemas/qualification-record.v1.json`](../../contracts/schemas/qualification-record.v1.json) | "May this version be used": a verdict made from a scorecard with pass^k and a run record |

Python is the reference implementation and the tests check it against the contracts: `run.py` (subset, baseline, `compare`, `validate_run_record`), `record.py` (`build_record`, `validate_record`, `check_model`).

## The drift canary: subset, baseline, run record (what C2 builds to)

| Document | Made by |
|---|---|
| Subset (`qualification-subset.v1`): the GENERATED, understanding-dependent cases (full case documents), sorted, with `cases_sha256` | `python -m benchmark.qualification subset --cases CASES.json --subset-id ID --out subset.json` |
| Baseline (`qualification-baseline.v1`): for each subset case, the slots the previously qualified, pinned version answered, plus the commit SHA | `... baseline --subset subset.json --answers answers.json --kind llm --provider P --model-id M --version V --commit-sha SHA --out baseline.json` |
| Run record (`qualification-run.v1`) | `... compare --subset ... --baseline ... --answers new-answers.json --kind ... --version NEW --commit-sha SHA --recorded-on DATE --out run.json` |

An answers file maps each subset `case_id` to `{"slots": {...}, "answered_by": "<version the provider reported>"}`, or to `null` when the provider did not answer.

- `pinned`: `kind`, `provider`, `model_id`, `version` the service was configured with. The version must be pinned (dated or numbered), never `latest`.
- `baseline`: the pinned version, commit SHA and digest of the baseline compared against. It must be for the same kind, provider and model ID (the version is what changes).
- `cases[]`: per case `status` (`MATCH`, `DRIFT`, `UNAVAILABLE`), `answered_by`, `version_mismatch`, `differing_slots` (`slot`, `expected`, `observed`; `expected` is absent for an extra slot and `observed` for a missing one), and the digests of the two answers.
- `result`: `DRIFT` if any case drifted (a different slot value, a missing or extra slot, or a version that answered under another version than the one asked for, even when every slot matches); else `PROVIDER_UNAVAILABLE` if any case got no answer; else `PASS`. Drift outranks unavailable.

**Boundaries.** The subset holds GENERATED cases only; `select_subset` refuses a held-out or partner case anywhere in its input (INV-22), so none of it reaches the model service. The baseline is a recorded model answer, never the oracle's expectation. The canary must send the subset through the same provider adapters the service uses; this module never calls a model.

## The qualification record

Made by `record` from a scorecard (`python -m benchmark.scoring ... --repeat-results` x4, so pass^k is available) and the run record. **Interim QUALIFIED (A17)** means all of:

1. the run is `complete`;
2. every Tier 0 gate is zero (`tier0_all_zero`);
3. the drift result is `PASS`;
4. pass^k (`available`, exactly five runs) is not below the **bar**, the higher of the pinned baseline's own pass^k (`--baseline-record`, which must be the QUALIFIED record of the version the run was compared against) and a Founder-set floor (`--floor` with `--floor-source`, the decision record that set it; the floor is pack data).

With no baseline record and no floor there is no bar: a **first qualification waits for a Founder-set floor** and the verdict is `NOT_QUALIFIED`. No number is chosen here (REQ-AGT-005). `decision_record` names the decision record that holds the qualification; `scorecard_sha256` and the drift `run_sha256` bind the record to the evidence.

```
python -m benchmark.qualification record --scorecard OUT/score/scorecard.json --run run.json --kind llm --provider P --model-id M --version V --baseline-record records/P__M__OLD__DATE.json \
    --decision-record DEC-... --qualified-on 2026-11-06 --out benchmark/qualification/records/P__M__V__2026-11-06.json
python -m benchmark.qualification check --records benchmark/qualification/records --provider P --model-id M --version V [--kind llm]
```

`record` exits 0 for QUALIFIED, 1 for NOT_QUALIFIED (the record is still written, so a failed qualification is on file), 2 for bad input. `check` exits 0 allowed, 1 refused, 2 unreadable input. Records live in [`records/`](records/README.md).

## The lookup contract for the model service (C2)

An ID may be used only if a valid `QUALIFIED` record matches its provider, model ID and version exactly, and the latest record for it (by `qualified_on`) is `QUALIFIED`. A later `NOT_QUALIFIED` revokes; two records of one day that disagree refuse; an unpinned version is refused before any lookup. `record.check_model` is the reference implementation. Three rules make it strict (Codex on the model-service port, relayed by C2): a version is pinned by the model service's own rule (`check_pinned`: the character set, a digit, and none of `latest`, `default`, `auto`, `stable`, `preview`, `current`, `newest` as a substring); a date is `YYYY-MM-DD` and nothing else, and dates are compared parsed; **an invalid record for the identity refuses the lookup (fail closed)** instead of being ignored, so a malformed NOT_QUALIFIED record still revokes. Flags are real booleans, and slot values are compared by JSON type (a boolean is never the number 1).

**What a PASS does not decide.** Whether a drift is acceptable, and whether the new version is then qualified, is the qualification record's decision record (a founder decision). A `PASS` in a run record is not a qualification.

## Tests

`python -m unittest discover -s benchmark/qualification -t . -p 'test_*.py'`. SYNTHETIC scorecards from the real scorer; generated layer only (INV-22). The tests validate real output against the contract schemas.
