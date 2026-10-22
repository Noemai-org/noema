# noema_model/understanding/pipeline

**What it does.** The fixed DAG of stateless steps (D3). Classify the intent node, then in parallel: the closed-vocabulary slots, the ambiguity-flag proposals, locale and code-switching, instruction-likeness screening and the slot values. Then the caller's normaliser and gate, a candidate read, and deterministic ranking. It proposes and never decides, writes a clarification question or checks policy.

**REQs.** REQ-INT-001, REQ-INT-002, REQ-INT-005, REQ-AMB-003, REQ-CTX-007, REQ-NFR-001; INV-01, INV-30; D3, D9.

| D9 task | Step | Goes through |
|---|---|---|
| 1 Intent node, including out of scope | `steps.classify_node` | `decide` |
| 2 `question_type`, `requested_remedy` | `steps.enumerated_slots` | `decide` |
| 3 Ambiguity-flag proposals | `steps.propose_flags` | `decide` |
| 4 Locale and code-switching | `steps.detect_locale` | `decide` |
| 5 Answer binding | `binding.propose_binding` | `decide` |
| 6 Instruction-likeness | `steps.screen_strings` | `decide`, signal only |
| Slot values (amount, party, time as said) | `steps.slot_mentions` | `generate` |

Which provider serves each task is harness configuration (`decide.<task>` routes); the bake-off chooses it, a founder adopts it.

## What the caller supplies

The pipeline owns none of these, and none is faked here:

- `CandidateSource`: implemented by `noema_model.evidence.EvidenceClient`, written to `candidates-request.v1.json` and `candidates-response.v1.json`. The pipeline never imports it (it imports no HTTP); the service wires it in. The contract requires a time window, so with none the pipeline reads nothing and raises the pack's time flag (`TIME_VAGUE` in the fintech pack; PERD §20.3), with no resolution: `NO_TRANSACTION_MATCH` is for a search that ran and came back empty (REQ-AMB-002).
- `Normalise`: the deterministic lexicon, whose values win over a model's (REQ-INT-003, UND-05).
- `Gate`: the schema gate on the assembled Intent Object (REQ-INT-001). Waiting on generated contract types (CON-03).

**A decision model's probability.** When the flag or screening task is served by a decision model, the answer is a bare probability. The pipeline counts it as a yes at or above `PipelineConfig.native_yes_threshold` (pack data; a benchmark and founder decision), and as a yes when none is set.

**Failure.** A provider failure or a refused reply on a required step is `REJECTED_AT_GATE` with the error code (`PROVIDER_UNAVAILABLE`, `SCHEMA_INVALID`), never a guess. A failed screening changes nothing. A candidate read over the bound is refused, not truncated. A counterparty filter that is empty or over 64 characters stops the turn with `INPUT_INVALID`: the request is never sent without the filter the customer's words asked for.

**Tests.** `tests/test_pipeline.py`; the arch-lint rule `pipeline-purity` keeps module state, HTTP, database and agent frameworks out.
