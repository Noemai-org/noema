# Contracts inventory

Every schema in `contracts/schemas/` has exactly one row, and every row names an existing schema (checked by `tools/contracts/validate.py`). A row says which SoT and PERD sections define the object and which requirements it serves. Schemas are JSON Schema 2020-12 and named `<name>.v1.json`. A contract change follows AGENT-15: a version bump, a migration note and regenerated models (codegen is CON-03).

## C-1 · Pack schema

| Schema | SoT | PERD | Requirements | Notes |
|---|---|---|---|---|
| `common.v1.json` | §43 | §32.3 | REQ-API-001 | Shared patterns: ID, hash, timestamp, signature, namespaced pack ID |
| `enums.v1.json` | §43.8, §15.3 | §25, §23.4 | REQ-API-001, REQ-CAP-001 | Capability and policy value lists; C-2 adds the rest |
| `pack.v1.json` | §15.5 | §20.4, §25 | REQ-API-001, REQ-AG-001 | The whole pack as data |
| `intent-node.v1.json` | §15.3 | §20.1, §20.4 | REQ-INT-005, REQ-AG-001 | Two nodes in the fintech pack (DEC-2026-10-16 C2) |
| `trust-profile.v1.json` | §15.3 | §23.2 | REQ-TRU-001, REQ-TRU-002 | |
| `outcome-definition.v1.json` | §15.3, §43.7 | §29 | REQ-VER-001, REQ-VER-002 | |
| `capability-descriptor.v1.json` | §43.8 | §25 | REQ-CAP-001, REQ-CAP-002 | Prohibited capabilities carry no executable fields |
| `evidence-source.v1.json` | §15.5, §43.5 | §21.4 | REQ-CTX-001, REQ-CTX-003 | |
| `policy-rule.v1.json` | §15.3 | §23.4 | REQ-TRU-001 | |
| `policy-version.v1.json` | §15.3, INV-26 | §23.4 | REQ-TRU-001 | |
| `clock.v1.json` | §15.3 | §23.5 | REQ-TRU-001 | SYNTHETIC until validated by counsel |
| `reason-catalogue-entry.v1.json` | §43.2 | §23.3 | REQ-TRU-001 | Pack tier of the reason codes (money-and-value-lists §2.3) |

## C-2 · Money and core enums

| Schema | SoT | PERD | Requirements | Notes |
|---|---|---|---|---|
| `money.v1.json` | §43.1 | §20.1 | REQ-API-001, REQ-DATA-002 | Integer minor units, 0 to 9007199254740991 (M1 to M4); `Quantity` keeps `value` and `unit` (M2, M8) |
| `enums.v1.json` (extended) | §43.1 to §43.5, §13.1 | §23.1, §23.3 | REQ-API-001, REQ-TRU-001 | Nineteen distinct named types; 17 of them are compared with `state-registry.md` by the drift guard (OD-P-10) |

Other files: `currencies.json` (the one exponent table, NGN = 2; M1) and `state-registry.md` (vendored copy of the normative registry, see its header).

M10 rows and where they are proven. Fixtures here cover the schema-level rows. The rest need code that does not exist yet:

| M10 row | Where |
|---|---|
| accept `0`, `1`, `2000000`, `9007199254740991` | `fixtures/valid/money/` (this PR) |
| reject `9007199254740992`, `-1`, `2000000.0`, `2e6`, `"2000000"`, `null`, currency missing, currency not in table, exponent conflict | `fixtures/invalid/money/` (this PR) |
| Go and Python decoders also reject the float literals | CON-03 (codegen) and CON-04 (vectors) |
| lexicon `"20k"` to `2000000`, `"₦50,000"` to `5000000` | UND-05 |
| `1234.567` NGN rejected at an adapter | the Tushe adapter task |
| addition crossing the ceiling, NGN plus another currency | the money arithmetic code (M6) |
| a JCS hash over `9007199254740991` identical in Go and Python | CON-04 |

## C-3 · Input, Intent Object, clarification

| Schema | SoT | PERD | Requirements | Notes |
|---|---|---|---|---|
| `canonical-input.v1.json` | §14 | §19.3 | REQ-INP-001, REQ-INP-002 | PERD names the file `canonical_input.v1.json`; this repository uses kebab-case for every file |
| `intent-slot.v1.json` | §43.1 | §20.1 | REQ-INT-001, REQ-AMB-001 | Five abstract slot types; a RESOLVED slot has `resolved_ref` and only a RESOLVED slot has one |
| `intent-object.v1.json` | §43.1 | §20.1 | REQ-INT-001, REQ-INT-004 | Version 1 supersedes nothing; a later version names `intent_id@version`; money in minor units |
| `clarification.v1.json` | §43.2 | §22.1 (REQ-AMB-004) | REQ-AMB-004, REQ-UI-002 | At most 4 options, no label with 5 or more consecutive digits, at most 2 rounds |
| `clarification-answer.v1.json` | §43.2 | §22.1 (REQ-AMB-005) | REQ-AMB-005 | Base fields, `clarification_id` and `answers` (slot name to a typed slot value); only slots the clarification requested (DEC-2026-10-18) |

`common.v1.json` gains one ID type per PERD §32.3 prefix (`IntentId`, `DecisionId`, `GrantId`, and so on), `IntentRef`, `ResourceRef`, `Ratio` and `PositiveInt`, so C-4 to C-6 add no change to it.

## C-4 · Decision Record, Decision Grant, confirmation

| Schema | SoT | PERD | Requirements | Notes |
|---|---|---|---|---|
| `decision-record.v1.json` | §43.2, §13.1 | §23.1, §23.3 (REQ-TRU-001) | REQ-TRU-001, REQ-TRU-010, REQ-AMB-001 | Seven outcomes; two-tier reason codes; `authority` results `PASS`, `FAIL`, `NOT_EVALUATED`; an ALLOW needs all three at `PASS` (DEC-2026-10-18); `disposition_decision_id` (SOT-C-04) |
| `decision-grant.v1.json` | §43.2 | §23 (REQ-TRU-008) | REQ-TRU-008, REQ-EXE-001 | Single use; binds principal, intent, capability, target, params, evidence set, plan step, versions |
| `confirmation-request.v1.json` | §43.2a | §23 (REQ-TRU-009) | REQ-TRU-009 | Bound to the exact action by `binding_hash` |
| `confirmation-event.v1.json` | §43.2a | §23 (REQ-TRU-009) | REQ-TRU-009 | Approver identity from authentication; `ACCEPT` or `DECLINE` |

## C-5 · Plan, Receipt, Verdict, recovery

| Schema | SoT | PERD | Requirements | Notes |
|---|---|---|---|---|
| `plan.v1.json` | §43.9 | §26 (REQ-PLN-001, REQ-PLN-002) | REQ-PLN-001, REQ-PLN-002 | The hashed object: no ID, time, tenant or correlation field, so identical inputs give an identical `plan_hash`; params are strings, integers, booleans or null |
| `execution-receipt.v1.json` | §43.3 | §27 | REQ-EXE-002, REQ-EXE-005, REQ-EXE-006 | Never `SUCCEEDED`; `idk_v1_` plus 64 hex |
| `outcome-verdict.v1.json` | §43.4 | §29 | REQ-VER-001, REQ-VER-003, REQ-VER-004 | `ACHIEVED` needs a true predicate and at least one read; a watch exists exactly on `PENDING_EXTERNAL` |
| `outcome-updated.v1.json` | §43.4a | §29 (REQ-VER-005) | REQ-VER-005, REQ-API-006 | The PERD table plus `schema_version`; summary masked |
| `recovery-request.v1.json` | §43.4 | §29 (REQ-VER-003) | REQ-VER-003, REQ-RCV-001 | Addressed to Trust only; exactly one of `verdict_id` (a verdict) or `grant_id` (a voided grant) (DEC-2026-10-18) |

## C-6 · Evidence, snapshot, Record, explanation facts, inbound events, errors

| Schema | SoT | PERD | Requirements | Notes |
|---|---|---|---|---|
| `evidence-item.v1.json` | §43.5 | §21.1 | REQ-CTX-001, REQ-CTX-002, REQ-CTX-005 | `read_at` and `effective_at` both required; `FactStatus` has no negative value (INV-19) |
| `evidence-payload.v1.json` | §43.5 | §38.2 | REQ-CTX-001, REQ-DATA-001 | Normalised JSON plus raw hash; insert-only |
| `context-snapshot.v1.json` | §14 | §21.3 | REQ-CTX-004 | `evidence_set_hash` per DEC-2026-10-16 C1; the empty set has the hash of zero bytes and no other set may |
| `record-event.v1.json` | §43.6 | §32.2 | REQ-AUD-001, REQ-AUD-002 | 53 event types of the PERD table plus 4 added by DEC-2026-10-18 (REQ-VOI-004, REQ-API-008, REQ-API-007, REQ-OPS-003); `INTENT_SUPERSEDED`, `STATE_CHANGED` and `RECORD_ACCESSED` are not events; `SECURITY_EVENT` needs a `kind` |
| `explanation-facts.v1.json` | §18.4 | §31.2 | REQ-EXP-001, REQ-EXP-002 | Masked facts only; the model never sees policy, thresholds or raw evidence |
| `inbound-event.v1.json` | §35.5 | §39.6 | REQ-API-008, REQ-CTX-005 | Smallest envelope the inbox rules need [REC]; payload is data |
| `noema-resolution.v1.json` | §39 | §39.5 | REQ-API-007 | `verified` true exactly when every listed verdict is `ACHIEVED` and at least one is listed |
| `errors.v1.json` | — | §39.1 | REQ-API-002 | 34 codes split into 5 retryable and 29 non-retryable (DEC-2026-10-18); no stack traces, no digit runs |

Not defined, for want of a field list in the PERD or SoT: the explanation render result (`text`, `claims[]`, REQ-EXP-001), the action graph (SoT §43.9), the operational memory record, and the integration (adapter) contract and scenario/evaluation case of SoT §43.10. Each needs a decision before a schema.

Gap decisions applied here: DEC-2026-10-18-con01-gap-closure (Noemai-org/docs#14). Each is enforced in a schema and covered by fixtures; `tools/contracts/test_gap_closure.py` covers what a schema cannot state (the retryable partition, the golden idempotency keys, the `recev` prefix, the Record event list).

## T2 · Action Graph, evidence descriptors, memory observation and integration contract

| Schema | SoT | PERD | Requirements | Notes |
|---|---|---|---|---|
| `action-graph.v1.json` | §43.9 | §24 | REQ-AG-001, REQ-AG-002, REQ-PLN-001 | Pack data. The PERD names the node fields but not the types of `on_unknown`, `on_failure`, `recovery_ref` and `escalation_ref`; smallest safe types chosen (PERD §30.1, §30.2) |
| `evidence-descriptor.v1.json` | §43.7 | §21.4, §24 | REQ-AG-001 | Typed fields per evidence entity so predicates type-check at pack load. Not in the SoT or PERD; added under AGENT-02 |
| `memory-observation.v1.json` | §35.1 | §33 | REQ-MEM-001, REQ-MEM-002 | The PERD lists the envelope fields only, not the fields of each kind; `observation` is held to typed scalars until each kind's owner supplies them (open decision) |
| `integration-contract.v1.json` | §39 | §39.5, §39.7 | REQ-API-001, REQ-API-006, REQ-API-007 | The three partner obligations, two scoped credentials and signed events. `noema.resolution` is `noema-resolution.v1.json` (C-6), referenced by name |

**Migration note (AGENT-15).** `pack.v1.json` gains four optional properties: `action_graph_files`, `routing_allowlist`, `evidence_descriptors` and `predicate_variables`. No existing property changes and no document valid before is invalid now, so the file stays `v1`. packcheck requires `evidence_descriptors` and `predicate_variables` when a pack holds any predicate.

**Migration note (AGENT-15), clarification.** `clarification.v1.json` gains an optional `mode` (`OPTIONS` default, `ATTRIBUTE`) and an `attribute` that ATTRIBUTE mode requires (DEC-2026-10-20 D4). Existing fields are unchanged and every earlier document stays valid, so the file stays `v1`.
