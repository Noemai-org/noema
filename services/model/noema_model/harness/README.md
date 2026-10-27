# noema_model/harness

**What it does.** The only road to a model (D2). Two interfaces beside each other: `generate` (one structured answer, for slot values and, later, explanations) and `decide(task, state, questions)` (D9: choice, score and yes/no questions for the six eligible understanding tasks). Every call renders a versioned template, masks and delimits customer and enterprise text, forces the output schema, tries the configured providers in order behind a circuit breaker, and returns a validated value with a full `CallRecord`.

**REQs.** REQ-INT-002, REQ-CTX-007, REQ-NFR-007, REQ-OBS-002, REQ-BEN-005; INV-01, INV-02, INV-29; AGENT-09, AGENT-13, AGENT-28.

## What it guarantees

| Rule | Where | Test |
|---|---|---|
| Only `harness/` imports `providers/` | the `harness-only-providers` lint | `make arch-lint` and its canary |
| A reply is validated whole and strictly; a mismatch is rejected, never repaired. NaN, Infinity, a repeated key, a code fence, trailing text, an extra or missing field all reject | `core._judge`, `_check_json` | `test_harness_call.py` (14 fixed cases plus a generated corpus) |
| A rejected reply fails over to the next provider; every provider failing is `PROVIDER_UNAVAILABLE`, retryable, never a heuristic guess | `core._call` | `test_harness_call.py` |
| Failover on timeout, 429, 5xx, 4xx, malformed envelope, an unpinned reply and a schema rejection; an open breaker is skipped | `core._call`, `breaker.py` | `test_harness_call.py` |
| A model version must be pinned in config, and the reply must report that exact version | `config.check_pinned`, `core._call` | `test_harness_config.py`, `test_harness_call.py` |
| Every text field is cleaned (control and format characters out), masked to the last four digits, capped, marked truncated and sent as JSON between sentinels with `<` and `>` escaped, so content cannot close the block | `fields.py` | `test_harness_fields.py` (properties) |
| A choice question without `none_or_other` is refused before any call; a task outside the six is refused | `decide_types.py`, `core.decide` | `test_harness_decide.py` |
| A probability is information. The harness holds no threshold and never turns one into a decision (a decision model's answer carries `yes=None`) | `decide_types.py`, `core._native_answers` | `test_harness_decide.py`, `test_openai_decisions.py` |
| Vendor reasoning output is dropped inside the adapter, so it cannot reach a record or a log | `providers/llm/*` | `test_providers_contract.py` |
| Cost comes only from a dated rate card, rounded up; with no rate or no token counts it says so. Every attempt carries its tokens, its price (the rate card used, with its date) and its cost; the call carries the totals. `CallRecord.to_dict()` is the JSON-safe form the Record and the benchmark runner read | `accounting.attempt_cost`, `cost_of`, `CallRecord.to_dict` | `test_harness_call.py`, `test_pipeline.py` |
| An output type must forbid extras, be strict and list every property as required | `core._schema_for` | `test_harness_call.py` |

## Configuration

`load_harness_config(dict)` reads JSON-shaped data and refuses anything missing or unknown. Nothing has a default. Shape:

```json
{
  "masking": {"account_run_min_digits": 8},
  "data_limits": {"default_max_chars": 256, "overrides": {"utterance": 2000}},
  "breaker": {"failure_threshold": 3, "cooldown_ms": 30000},
  "providers": {
    "decisions": {"kind": "openai_decisions", "endpoint": "https://...", "model_version": "gpt-6-luna",
                  "credential_file": "/run/secrets/openai_key", "timeout_ms": 1200, "max_response_bytes": 65536},
    "gpt": {"kind": "openai_chat", "endpoint": "https://...", "model_version": "<a pinned snapshot>",
            "credential_file": "/run/secrets/gpt_key", "timeout_ms": 1200, "max_output_tokens": 512,
            "max_response_bytes": 65536,
            "rate": {"input_micro_per_mtok": 0, "output_micro_per_mtok": 0, "currency": "USD", "as_of": "2026-10-01"}}
  },
  "routes": {
    "slot_values": {"default": ["gpt"], "by_locale": {"yo": ["gpt"]}},
    "decide.intent_node_classification": {"default": ["gpt"], "by_locale": {}}
  }
}
```

The numbers above are placeholders to show the shape, not product settings. `kind` is `openai_chat`, `anthropic_messages` (needs `api_version`) or `gemini_generate`. A route key is `slot_values` (or another generate route) or `decide.<task>` for each of the six tasks. To point a provider at a test endpoint, change its `endpoint`; nothing in this package knows any endpoint exists.

## Decision models (D9): the two implementations of `decide()`

`decide()` has exactly two implementations behind one interface: the **LLM-backed** one (any `openai_chat`, `anthropic_messages` or `gemini_generate` provider, answering in a forced schema) and the **OpenAI Decisions** one (`kind: openai_decisions`, a `DecisionProvider`). Both appear in a route (`decide.<task>`), in the order configured, so the LLM is the baseline and the fallback. The adapter is written from OpenAI's own reference (read 22 Oct 2026; `tests/fixtures/providers/README.md`): `POST /v1/decisions`, bearer token, `predicate`, `choice` and `score` questions, a per-question `refusal`.

| Our question | Sent as | Comes back as |
|---|---|---|
| `YesNoQuestion` | `predicate` | `YesNoAnswer(yes=None, p_yes)`: a bare probability |
| `ChoiceQuestion` | `choice`; each option id is a choice value and its label the description; `none_or_other` is an ordinary choice value, as the guide recommends ("include a fallback option such as other") | `ChoiceAnswer(selected, probabilities, confidence)`: the provider's own `choice`, its distribution and its separate `confidence` |
| `ScoreQuestion` (needs two or more levels) | `score` with the levels in order | `ScoreAnswer(score, probabilities level_0.., confidence)`: the provider's own `score`. The LLM-backed path returns only the distribution and no score, because the harness computes none |
| any of them, refused by the model | a `refusal` answer | The whole set is treated as a refused reply (`OUTPUT_REFUSED`) and the next provider is asked |

- **Limits are the provider's, not ours.** A provider exposes `accepts(questions)`; the OpenAI adapter takes 1 to 400 questions and 2 to 255 unique choices (the reference's limits). A set it does not accept is recorded as `UNSUPPORTED` (never made, free, no breaker charge) and the next provider is asked.
- **Probabilities are information.** The harness never turns one into a decision. A decision model's yes/no answer carries `yes=None`; the pipeline reads it against the pack's `native_yes_threshold` (a benchmark and founder decision, AGENT-16), and with none configured counts it as a yes, which only adds caution or a logged signal. A choice answer's `selected` is the provider's own field; its `confidence` is passed through, and no pack threshold on it exists yet.
- **No dated snapshot exists (gap).** The model page lists one snapshot, `gpt-6-luna` ("Default snapshot: gpt-6-luna"; "Snapshots: gpt-6-luna"), and the endpoint says only that model is supported. The reply-equals-pin check therefore cannot see a silent change of weights behind the name. The pinned-version rule is unmet for this provider until a dated snapshot exists or the founders accept the alias; the API is in public beta ("we expect to GA in the coming weeks").
- **Price.** The guide and model page give $0.10 per 1M input tokens and no output, cache or reasoning charge, so a rate card is `{"input_micro_per_mtok": 100000, "output_micro_per_mtok": 0, "currency": "USD", "as_of": "2026-10-22"}`. Not modelled: the 10% regional-processing premium, the 2x multiplier above 272K input tokens, and the Flex, Batch and Fast tiers.
- **Data handling (PERD R-18).** The data-controls page lists `/v1/decisions` as not used for training, with abuse-monitoring logs kept up to 30 days by default and no application state; Zero Data Retention is available to approved customers "with limitations", and "Private Retention" eligibility is "pending confirmation". Residency is available for the US and EEA plus Switzerland. Until a data-terms review, synthetic data only.
- **Errors.** The adapter reads no error body: any non-2xx status maps to a failure kind (408 timeout, 429 rate limited, 5xx unavailable, others rejected) and the next provider is asked. `Retry-After` is not honoured; failover replaces waiting. A 429 for credit or spend limits is not retried by waiting either, per the error-codes page.
- Microsoft-Decision-1 and Jev adapters are not built (scope update); they come only if the bake-off needs a second option.
- Not here: the explanation and voice callers.

**Tests.** `pytest` from `services/model/`.

## Cost files (bench runs only)

Every model call emits one `MODEL_CALL_RECORDED` event whose `record` field is `CallRecord.to_dict()` as JSON. During a bench run, `platform.telemetry.cost_files_from_environment(telemetry, environ)` wraps the telemetry when `BENCH_OUT` is set (the Makefile's `BENCH_OUT`, default `benchmark/out`) and writes each record to `$BENCH_OUT/cost/<correlation_id>-<NNN>.json`, created exclusively so no file is overwritten. The name is exact: a correlation id with a character outside `[A-Za-z0-9_-]`, an empty one, or one over 246 characters is refused (`CostFileRefused`), never rewritten or cut, so two ids never share a name; NNN runs 001 to 999 and the 1000th call for one id is refused. The benchmark reader applies the same rules. With `BENCH_OUT` unset or empty the telemetry is returned unchanged and nothing is written. A write that fails raises, so a run with missing cost cannot look complete. The file is accounting for the benchmark, never proof of an outcome (AGENT-21).

`noema_model/main.py` calls it through `build_telemetry(os.environ)` and keeps the result on `app.state.telemetry`; `make model-bench-up` is the only thing that sets `BENCH_OUT` for the container. Nothing constructs the `Harness` in the service yet, so no running service writes a file until that request path exists.
