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

`decide()` has exactly two implementations behind one interface: the **LLM-backed** one (any `openai_chat`, `anthropic_messages` or `gemini_generate` provider, answering in a forced schema) and the **OpenAI Decisions** one (`kind: openai_decisions`, a `DecisionProvider`). Both appear in a route (`decide.<task>`), in the order configured, so the LLM is the baseline and the fallback.

What the OpenAI Decisions adapter covers is only what the founder's pasted example shows (`tests/fixtures/providers/README.md`): `POST /v1/decisions` with `predicate` (yes/no) questions, answered with one probability each.

- **Yes/no sets only.** A question set with any choice or score question is not sent to a decision model: the attempt is recorded as `UNSUPPORTED` (never made, free, no breaker charge) and the next provider in the route is asked. No choice or score format is held, so none is guessed. Until it is, the four choice tasks (node, enumerated slots, locale, binding) run on the LLM-backed path, and only the two yes/no tasks (ambiguity flags, instruction-likeness) can use the decision model.
- **A bare probability.** The decision model returns no selection, so `YesNoAnswer.yes` is `None` and `p_yes` is the probability, untouched. The harness never turns it into a yes or a no. The pipeline does, with the pack's `native_yes_threshold` (a benchmark and founder decision, AGENT-16); with none configured it counts as a yes, because a yes here only adds caution or a logged signal.
- **Version pinning gap.** The example names one model, `gpt-6-luna`, and the response echoes the same name. No dated snapshot is shown, so the reply-equals-pin check cannot see a silent change of weights behind that name. This is recorded as a gap: the pinned-version rule still applies before any Record-relevant use, and the adapter must not be adopted for a task until a snapshot exists or the founders accept the alias (D9).
- Microsoft-Decision-1 and Jev adapters are not built (scope update); they come only if the bake-off needs a second option.
- Not here: the explanation and voice callers.

**Tests.** `pytest` from `services/model/`.
