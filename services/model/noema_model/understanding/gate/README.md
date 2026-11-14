# noema_model/understanding/gate

The model service's side of the schema gate (UND-04; REQ-INT-001, REQ-INT-002, REQ-INP-002; INV-02, INV-12). The core has the second gate (noema#453, `services/core/understanding`); this is the first. Updated 13 Nov 2026.

**What it does.** `SchemaGate.check(raw, Context) -> GateResult`. `raw` is the Intent Compiler's proposal: an Intent Object with status `EXTRACTED` and **no** `sig`. The result is `ok` with the object as `VALIDATED`, or not `ok` with a sorted, unique list of `Reason` values and no object. It repairs nothing, signs nothing, has no clock and never raises (any bytes give a result).

| Step | Rule | Reason |
|---|---|---|
| Context | the tenant, correlation id, principal id and session id the canonical input carries are well formed (a caller bug, not the model's) | `CONTEXT_INVALID` |
| Size | not empty, not over the configured bound | `SIZE` |
| Parse | one UTF-8 JSON document, no NaN or Infinity, no trailing data | `NOT_JSON` |
| Shape | one object; no member twice; decoded by the generated strict `IntentObjectV1` (closed, no null where the contract has none, every pattern and range) | `SHAPE` |
| Identity members | a `principal_type` or `asserted_by` in the proposal is refused (A166); the gate fills both with `UNSET` for the closed decode | `SHAPE` |
| Signature | a `sig` in the proposal is refused: the model service never signs; the core hashes and signs after its own gate (ruling A166, item 1) | `SIG_SET_BY_MODEL` |
| State | status is `EXTRACTED`; `schema_version` is `3.0` | `STATUS_NOT_EXTRACTED`, `SCHEMA_VERSION` |
| Chain | version 1 supersedes nothing; version n names `intent_id@(n-1)` | `VERSION`, `SUPERSEDES` |
| Time | `created_at` is a real instant | `CREATED_AT` |
| Identity | tenant, principal id and session id equal the Context's; the correlation id equals the Context's | `IDENTITY_MISMATCH`, `CORRELATION_MISMATCH` |
| Source | Mode 1 only; `raw_ref` at most 256 characters | `MODE_NOT_ENABLED`, `SOURCE` |
| Pack | the pack id and version equal the loaded pack's; the node is the pack's; every slot is named by the node and of its type; outcomes are the node's, none twice | `PACK_MISMATCH`, `UNKNOWN_NODE`, `SLOT_NAME`, `UNKNOWN_SLOT`, `SLOT_TYPE`, `OUTCOME_NOT_IN_NODE` |
| Slots | a Resource is RESOLVED if and only if it has a `resolved_ref` (INV-06); a TimeRef window is two real instants | `SLOT_INVALID` |
| Flags | no ambiguity flag twice | `AMBIGUITY_FLAG` |

The value of `sig` in a validated proposal is the marker `UNSIGNED` (the contract requires a `sig`; the core, not this service, builds the object, hashes it and signs it, and its gate refuses any `sig` but the marker). A reason names the rule and never quotes the object.

**Identity members the model does not send (ruling A166, item 3).** The proposal carries no `principal_type` and no `asserted_by`: the core sets them from the session (INV-12). A proposal that has either, even with the session's value, is a `SHAPE` refusal, and nothing raises a security event on a normal compile. The contract still requires the two members, so a validated object holds the neutral marker `UNSET` (`"unset"`) in both, which the core overwrites.

**Not here.** The prompt, the provider call and the assembly of the object (UND-05, next slice); the signature; the HTTP route is `../compiler/`; the pack loader (`PackView` is a protocol; the real pack nodes are PCK-01's and `packs/fintech/intents/` is empty); the intent hash (the core's, noema#454).

**Contract version, and what this gate validates.** The model half validates the proposal as an **`intent-object.v3`-shaped document** (`IntentObjectV3`, slots by `intent-slot.v2`; confidence is an integer in basis points, 0 to 10000, ruling A173, because the canonical JSON the Record signs refuses non-integers) whose two identity members carry the neutral marker `UNSET`. It is the proposal, not the object the core admits: **the core builds the object** (the core's gate still decodes v1 until its own move to v3, a separate change), writes the identity from the session, then hashes and signs it (ruling A166, items 1 and 5). A dedicated proposal contract would be a contract change (AGENT-15) with C1 as co-owner; none is proposed.

**Tests.** `pytest tests/test_intent_gate.py` from `services/model/`: a 30-row rejection table, every member missing, unknown members at five depths, a 10,000-case unknown-member fuzz, totality and determinism over arbitrary bytes, pathological input, and "no reason carries text of the object". `mypy --strict` is clean.
