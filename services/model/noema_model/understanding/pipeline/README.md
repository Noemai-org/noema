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

- `CandidateSource`: implemented by `noema_model.evidence.EvidenceClient`, written to `candidates-request.v1.json` and `candidates-response.v1.json`. The pipeline never imports it (it imports no HTTP); the service wires it in. The contract requires a time window, so with none (A535, interim: no default window and no refusal) nothing is read, no resolution is reported and the turn is `PROPOSED` with the resource slot unresolved for Trust to ask about: `NO_TRANSACTION_MATCH` is for a search that ran and came back empty (REQ-AMB-002).
- `Normalise`: the deterministic lexicon, whose values win over a model's (REQ-INT-003, UND-05).
- `Gate`: the schema gate on the assembled Intent Object (REQ-INT-001). Waiting on generated contract types (CON-03).

**A decision model's answers.** Any of the six tasks can be served by a decision model (routes `decide.<task>`, the LLM as fallback). A choice answer carries the provider's own selection, so it is used like an LLM's; a yes/no answer (flags, screening) is a bare probability. The pipeline counts it as a yes at or above `PipelineConfig.native_yes_threshold` (pack data; a benchmark and founder decision), and as a yes when none is set.

**Failure.** A provider failure or a refused reply on a required step is `REJECTED_AT_GATE` with the error code (`PROVIDER_UNAVAILABLE`, `SCHEMA_INVALID`), never a guess. A screening that did not run (outage, timeout, a reply that breaks the schema, no route) is a rejection like any other required step, for the utterance and for the candidates' display names alike (A372, relayed): no proposal, no candidate read after it, and the failed call stays in the call list. A finding is still only a signal. A candidate read over the bound is refused, not truncated. A counterparty filter that is empty or over 64 characters stops the turn with `INPUT_INVALID`: the request is never sent without the filter the customer's words asked for.

**Tests.** `tests/test_pipeline.py`; the arch-lint rule `pipeline-purity` keeps module state, HTTP, database and agent frameworks out.

**A screen that did not run (A372, A437).** A refused screen (outage, timeout, a reply that broke the schema, no route) ends the turn `REJECTED_AT_GATE` with the harness's code, as for any required step. The result also names it in `screen_not_run` (`UTTERANCE`, `CANDIDATES`, or the utterance's when both it and another step were refused); that is the internal class `SCREEN_NOT_RUN` of A437. The compiler raises `ScreenNotRun`, a `ProviderUnavailable`, so the public answer does not change. A finding (the screen ran and flagged) is not a screen that did not run.
