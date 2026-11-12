# noema_model/understanding/gate

The model service's side of the schema gate (UND-04; REQ-INT-001, REQ-INT-002, REQ-INP-002; INV-02, INV-12). The core has the second gate (noema#453, `services/core/understanding`); this is the first. Updated 12 Nov 2026.

**What it does.** `SchemaGate.check(raw, Context) -> GateResult`. `raw` is the Intent Compiler's proposal: an Intent Object with status `EXTRACTED` and **no** `sig`. The result is `ok` with the object as `VALIDATED`, or not `ok` with a sorted, unique list of `Reason` values and no object. It repairs nothing, signs nothing, has no clock and never raises (any bytes give a result).

| Step | Rule | Reason |
|---|---|---|
| Context | the tenant, correlation id, principal id and session id the canonical input carries are well formed (a caller bug, not the model's) | `CONTEXT_INVALID` |
| Size | not empty, not over the configured bound | `SIZE` |
| Parse | one UTF-8 JSON document, no NaN or Infinity, no trailing data | `NOT_JSON` |
| Shape | one object; no member twice; decoded by the generated strict `IntentObjectV1` (closed, no null where the contract has none, every pattern and range) | `SHAPE` |
| Signature | a `sig` in the proposal is refused: the model service signs after the gate | `SIG_SET_BY_MODEL` |
| State | status is `EXTRACTED`; `schema_version` is `1.0` | `STATUS_NOT_EXTRACTED`, `SCHEMA_VERSION` |
| Chain | version 1 supersedes nothing; version n names `intent_id@(n-1)` | `VERSION`, `SUPERSEDES` |
| Time | `created_at` is a real instant | `CREATED_AT` |
| Identity | tenant, principal id and session id equal the Context's; the correlation id equals the Context's | `IDENTITY_MISMATCH`, `CORRELATION_MISMATCH` |
| Source | Mode 1 only; `raw_ref` at most 256 characters | `MODE_NOT_ENABLED`, `SOURCE` |
| Pack | the pack id and version equal the loaded pack's; the node is the pack's; every slot is named by the node and of its type; outcomes are the node's, none twice | `PACK_MISMATCH`, `UNKNOWN_NODE`, `SLOT_NAME`, `UNKNOWN_SLOT`, `SLOT_TYPE`, `OUTCOME_NOT_IN_NODE` |
| Slots | a Resource is RESOLVED if and only if it has a `resolved_ref` (INV-06); a TimeRef window is two real instants | `SLOT_INVALID` |
| Flags | no ambiguity flag twice | `AMBIGUITY_FLAG` |

The value of `sig` in a validated object is the marker `UNSIGNED` (the contract requires a `sig`; whoever signs replaces it). A reason names the rule and never quotes the object.

**Not compared.** `principal_type` and `asserted_by`: the canonical input does not carry them, so the model cannot know them. The core overwrites them. How the compiler fills them, and whether the core counts a difference as an override attempt, is point 3 of noema#484.

**Not here.** The prompt, the provider call, the assembly of the object, the signature and the HTTP route (UND-05, noema#484); the pack loader (`PackView` is a protocol; the real pack nodes are PCK-01's and `packs/fintech/intents/` is empty); the intent hash (the core's, noema#454).

**Contract version.** Decodes `IntentObjectV1`. After intent-object.v2 (noema#480, ruling A163) is on `main` this moves to `IntentObjectV2` together with the core's gate.

**Tests.** `pytest tests/test_intent_gate.py` from `services/model/`: a 30-row rejection table, every member missing, unknown members at five depths, a 10,000-case unknown-member fuzz, totality and determinism over arbitrary bytes, pathological input, and "no reason carries text of the object". `mypy --strict` is clean.
