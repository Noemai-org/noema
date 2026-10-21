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

- `CandidateSource`: the client of the core's Evidence API (REQ-CTX-006). Waiting on the T2 candidate-read contract.
- `Normalise`: the deterministic lexicon, whose values win over a model's (REQ-INT-003, UND-05).
- `Gate`: the schema gate on the assembled Intent Object (REQ-INT-001). Waiting on generated contract types (CON-03).

**Failure.** A provider failure or a refused reply on a required step is `REJECTED_AT_GATE` with the error code (`PROVIDER_UNAVAILABLE`, `SCHEMA_INVALID`), never a guess. A failed screening changes nothing. A candidate read over the bound is refused, not truncated.

**Tests.** `tests/test_pipeline.py`; the arch-lint rule `pipeline-purity` keeps module state, HTTP, database and agent frameworks out.
