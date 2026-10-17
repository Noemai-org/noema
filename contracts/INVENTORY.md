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

## C-4 · Decision Record, Decision Grant, confirmation

## C-5 · Plan, Receipt, Verdict, recovery

## C-6 · Evidence, snapshot, Record, explanation facts, inbound events, errors
