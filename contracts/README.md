# contracts

Canonical, versioned contracts (JSON Schema 2020-12, `<name>.v<major>.json`). Owner series: CON-01.

- `schemas/`: the schemas (CON-01: C-1 to C-6, the candidates read contract and the reconstruction response contract).
- `fixtures/valid/` and `fixtures/invalid/`: one of each per schema.
- `fixtures/golden_vectors.json`: existing evidence-set hash vectors; see `fixtures/README.md`.

Changes need both founders (CODEOWNERS). A contract change follows AGENT-15: version bump, migration note, regenerated models.

**Vendored copies of docs files.** These files are copies of originals in `Noemai-org/docs`; the original wins on any difference. Each starts with a comment naming the docs commit it was taken from: `AGENTS.md`, `CLAUDE.md`, `contracts/state-registry.md` and `contracts/perd-32-2-event-types.md` (an extract of PERD section 32.2). One command re-points all of them to a clean docs checkout: `python3 tools/vendor/repoint_docs.py --docs <docs checkout>` (add `--check` to only report differences). The current PERD file for the extract is read from the docs checkout's `docs/poc/perd/README.md` (the row marked "Current."), so a PERD version bump needs no change to the script.

## Record event v2 (27 Oct 2026)

`schemas/record-event.v2.json` and `schemas/reconstruction.v2.json` sit beside their v1 files, which are not edited (AGENT-15; a test pins their bytes). Decision record: `DEC-2026-10-27-record-event-v2` in `Noemai-org/docs`.

- **What v2 is.** The PERD §32.1 event: the SoT §43.6 fields plus `schema_version` (major 2), `tenant_id`, `seq`, `payload_hash` and `actor`; the first event of a tenant's chain carries `sha256:` plus SHA-256 of `"noema:genesis:" ‖ tenant_id` as `prev_hash` (64 zeros is the v1 value and is invalid in v2). `GrantVoidReason` has nine values, one per reason PERD §27.1 and REQ-EXE-003 name; an unknown value is rejected. The event types are unchanged and are held equal to the vendored PERD §32.2 by the drift guard.
- **`reconstruction.v2`** differs from v1 only in that its events (and the timeline's event type) are `record-event.v2`.
- **Migration note.** Breaking (new required fields, a different genesis rule, a larger enum), hence a new major. A document is v1 or v2, never both: a v1 event does not validate as v2 and the reverse (tests). Writers, verifier and readers move together: `services/core/record/chain` (writer and `Verify`), `execution/ledger`, and the benchmark runner's reconstruction reader (`benchmark/runner/reconstruction.py`, now `reconstruction.v2`). No generated models exist yet (CON-03). No migration tooling: this is the synthetic POC.
- **Generated files.** The v2 fixtures and the `record_event_chain_vectors` family of `fixtures/golden_vectors.json` are written by `python3 -I tools/record_chain_ref.py --write` from PERD §32.1, with every hash computed. `--check` (run by `tools/contracts/test_record_event_v2.py`) fails if a file differs. No other golden-vector family is touched, so no existing digest changes and the test-only key of `grant_signing_vectors` is unchanged.
- **Tooling.** `tools/contracts/validate.py` and `benchmark/contracts.py` read `<name>.v<major>.json`; fixture folders are `<name>` for v1 and `<name>.v<major>` after.

## Record event v3 (27 Oct 2026)

`schemas/record-event.v3.json` and `schemas/reconstruction.v3.json` sit beside v1 and v2, which are not edited (AGENT-15; a test pins their bytes). Decision record: `DEC-2026-10-27-actor-and-payload-hash` in `Noemai-org/docs`; the text they follow is PERD v1.17 §32.1.

- **What v3 is.** v2 with the emitter, the principal and `payload_hash` defined. `schema_version` is major 3. `written_by` is one of 17 service identities (the ten of the PERD §41.1 table and the seven §34.1 logical services whose adapters carry an identity; `fault-controller` and `fixture-loader` are benchmark-only and not emitters). The machine-readable table is `services/core/record/chain/actors.json`; the core derives its sets from it and accepts 16 of the 17 (the benchmark runner's identity is in the contract and refused by the core). `actor` is required and is `<kind>:<id>` (kind: customer, operator, approver, auditor, regulator, admin; id the session token's `sub`, no colon) or, when no authenticated person caused the event, the emitter's own identity, equal to `written_by` (a rule per emitter in the schema). `service` is not a principal kind. `payload_hash` is SHA-256 of the UTF-8 RFC 8785 form of the payload, integers only; an event that refers to no object has the payload `{}`. The event types, security kinds, nine grant-void reasons and failover reasons are those of v2.
- **Migration note.** `actor` changes meaning and form, so v3 is not additive (new major, v2 unchanged). No v2 event exists outside tests and fixtures, so nothing is migrated: the writer (`services/core/record/chain`), the verifier, the ledger (emitter `core-execution`) and the benchmark runner's reader (`reconstruction.v3`) move together. No generated models exist yet (CON-03).
- **Generated files.** `python3 -I tools/record_chain_ref_v3.py --write` writes the v3 fixtures and the `record_event_chain_vectors_v3` family; `--check` (also run by `tools/contracts/test_record_event_v3.py`) fails on any difference. v2 is still written by `tools/record_chain_ref.py`, and its output did not change. The `payload_hash_vectors` family comes from `tools/payload_hash_independent.js` (Node), not from the generator.
- **Tooling.** `tools/jcs_ref.py` is the Python RFC 8785 for the generators: it sorts members by UTF-16 code unit and refuses any number that is not an integer within 2^53-1.

## Intent Object v2 (12 Nov 2026)

`schemas/intent-object.v2.json` sits beside `intent-object.v1.json`, which is not edited (AGENT-15; `tools/contracts/test_intent_object_v2.py` pins its bytes). Ruling A163 of the Architect (12 Nov 2026; the question is noema#475, asked by C1). Task A163.

- **What v2 is.** v1 with `schema_version` major 2 and one relaxed pattern: `principal.asserted_by` is `^[a-z][a-z0-9_-]*$`, so it may contain `-`. The session's verified issuer is `noema-poc-idp` (ruling A1, exact match), which v1's `^[a-z][a-z0-9_]*$` refused. The new pattern is as strict as the old one everywhere else: it starts with a lower-case letter and allows no dot, slash, colon, space, upper-case letter or non-ASCII character (a test lists the edges). A test fails if v2 differs from v1 anywhere but `$id`, the top description, `schema_version` and that pattern.
- **Not changed.** `principal_id` (`usr_…`), `principal_type` and `authn_ref`. Whether the edge refuses a `sub` that is not `usr_…`, or the contract is relaxed, is still open on noema#475.
- **Migration note.** Relaxing a pattern is not breaking for a reader of v2, but a v1 reader refuses a v2 object that uses `-`, and the major in `schema_version` says which one a document is. A document is v1 or v2, never both (tests). No Intent Object exists outside tests and fixtures, so nothing is migrated. The model service still compiles v1 (it has no compile route yet); the core gate (noema#453) decodes `IntentObjectV1` and does not enforce patterns, so it accepts a v2 object's identity block unchanged. Moving the gate to `IntentObjectV2` is a follow-up that lands with the Go-side pattern validation (A158), after both PRs are on `main`.
- **Fixtures and models.** `fixtures/{valid,invalid}/intent-object.v2` are every v1 fixture with `schema_version` 2.0, plus a valid hyphenated issuer and invalid `asserted_by` cases (upper case, leading hyphen, leading digit, dot, slash, space, empty, trailing newline) and a major-1 object. Generated Go and Pydantic models come from `make codegen`; `tools/codegen/shape_refusals.json` is regenerated by `tools/codegen/shape_refusals.py --write`.

## Intent Object v3 and Intent Slot v2 (13 Nov 2026)

`schemas/intent-slot.v2.json` and `schemas/intent-object.v3.json` sit beside `intent-slot.v1.json`, `intent-object.v1.json` and `intent-object.v2.json`, none of which is edited (AGENT-15; `tools/contracts/test_intent_object_v3.py` pins their bytes, and `common.v1.json`'s). Ruling A166 (2) of the Architect (recorded in docs#159) and A173. Task CON-01 (the contracts). PR noema#520.

- **What it is.** Every confidence is an integer number of basis points, a new def `BasisPoints` (integer, 0 to 10000; 1 = 0.01 percent) in `intent-slot.v2`. `intent-slot.v2` is v1 with the `confidence` of its five slot shapes as `BasisPoints`; `intent-object.v3` is `intent-object.v2` with `schema_version` major 3, `slots` from `intent-slot.v2` and `source.asr_confidence` as `BasisPoints` (null stays legal: a text turn has none). `asserted_by` is as in v2 (it allows `-`). `common.v1` and its fractional `Ratio` are not edited, and `canonical-input.v2` keeps `Ratio` (it is handed to the model and never hashed or signed).
- **Why.** `crypto.CanonicalJSON` refuses a fractional number before it canonicalises, so no hash or signature can be computed over an object that holds `0.93`. A166 (2): the fix is in the data, not in the canonical rule. A test shows both halves (a v2 object with a confidence cannot be canonicalised; every valid v3 fixture can).
- **Who converts.** The producer, at its boundary: the model's proposal asks for integer basis points, and the schema gate refuses a fractional number as a shape error (it never rounds model output). The ASR adapter converts the engine's ratio to basis points at its boundary. Neither adapter exists yet; nothing converts in this PR.
- **Migration note.** A new major (the type of a field changes), hence v3; v1 and v2 stay valid for what they describe. A document is v2 or v3, never both: a v3 object does not validate as v2 (its confidences are over 1) and a v2 object does not validate as v3 (its version and its ratios). No Intent Object exists outside tests and fixtures, so nothing is migrated. The model service and the core gate still decode v1 (the gate moves to `IntentObjectV3` with the A158 Go-side validation, a follow-up).
- **Fixtures and models.** `fixtures/{valid,invalid}/intent-object.v3` are every v2 fixture with the scale and the version changed, plus the edges (0 and 10000, a null ASR confidence; fractional, over range, negative, string, boolean, in a slot and in the source, and a major-2 object). `fixtures/{valid,invalid}/intent-slot.v2` do the same for the slot and the `BasisPoints` def. Generated Go and Pydantic models come from `make codegen`; `tools/codegen/shape_refusals.json` is regenerated by `tools/codegen/shape_refusals.py --write`.

## Decision Record v2 (6 Nov 2026)

`schemas/decision-record.v2.json` sits beside `decision-record.v1.json`, which is not edited (AGENT-15; `tools/contracts/test_decision_record_v2.py` pins its bytes). Rulings A5 and A13 of 6 Nov 2026 (record: docs#117, still Proposed, so noema#168 says "do not merge before docs#117"). Task TRU-05.

- **What v2 is.** v1 with `schema_version` major 2, two new required fields, `issued_at` and `expires_at` (simulated clock), and a defined `sig` (A13): HMAC-SHA-256 with the grant keys over the domain label `noema/decision-record/v2`, one `0x00` byte, and the RFC 8785 (JCS) form of the record without `sig.value`. `sig.key_id` stays in the signed bytes, so a signature cannot be re-attributed to another key. The bound fields are `intent_id`, `correlation_id`, `policy_version` (`policy_id@version`), `issued_at`, `expires_at` and `key_id`; a record missing any is invalid. v1 already carried `intent_id`, `correlation_id` and `policy_version`; A5's "adds" is read as "requires in v2".
- **Migration note.** v1 records have no defined signature rule and no `issued_at`/`expires_at`, so v2 is a new major and v1 is not edited. A v1 record does not validate as v2 (tests). The reverse is not true: v1's version pattern is generic, so a v2 record also passes v1's pattern; readers dispatch on `schema_version`. No v1 record exists outside fixtures; the Decision Record writer (TRU-05 Go side, not built) writes v2 only. No migration tooling: this is the synthetic POC.
- **Fixtures and models.** `fixtures/{valid,invalid}/decision-record.v2` are the v1 fixtures with version 2.0 and the two new fields, plus invalid records that miss each bound field or carry major 1. Generated Go and Pydantic models come from `make codegen`.

## Benchmark diagnostics v2 (2 Nov 2026)

`schemas/benchmark-case-result.v2.json` and `schemas/benchmark-scorecard.v2.json` sit beside their v1 files, which are not edited (AGENT-15; `tools/contracts/test_benchmark_v2.py` pins their bytes). Task BEN-30; PERD REQ-BEN-009; decision record `DEC-2026-10-31-proof-depth-and-completeness` (D5, D6); approved by the Architect on 2 Nov 2026.

- **What v2 is.** The case result gains the required integer `turns` (customer turns the runner sent through the public API). The scorecard's `tier1.diagnostics` gains `pass_k` (k fixed at 5; `available: false` with a reason when 5 runs were not supplied, never computed from another k), `turns_to_resolution`, `unnecessary_clarification`, `auto_resolve_precision` and `definitions` (the pre-registered definitions applied). Diagnostics, not gates; no threshold (AGENT-16).
- **Migration note.** Breaking (a required member is added; `additionalProperties` is false), hence a new major. A document is v1 or v2, never both (tests). The runner, the scorer, its report and CLI move together. `benchmark/replay` (noema#102) validates v1 results and moves to v2 in whichever pull request merges second.
- **Fixtures.** `fixtures/{valid,invalid}/benchmark-case-result.v2` and `benchmark-scorecard.v2`: every v1 fixture with the new members, plus new valid and invalid cases. No generated models exist yet (CON-03).

## Benchmark scorecard v3 (13 Nov 2026)

`schemas/benchmark-scorecard.v3.json` sits beside v1 and v2, which are not edited (AGENT-15; `tools/contracts/test_benchmark_v2.py` and `test_benchmark_v3.py` pin their bytes). Ruling A190 (interim, Founder-approved); task BEN-30; PERD REQ-BEN-004.

- **What v3 is.** v2 plus one required member on every rate, `exact_upper95`: the exact (Clopper-Pearson) one-sided 95% upper bound on the true rate of the counted failure (`1 - 0.05^(1/n)` at zero failures, which SoT section 38.8 approximates by `3/n`). Rounded up to six places; null when n is 0 or the rate is not a rate of failures. It is reported, not a gate: no threshold is added or changed (AGENT-16), and `rule_of_three_upper` stays.
- **Migration note.** Breaking (a required member is added; `additionalProperties` is false), hence a new major. A document is v2 or v3, never both (tests). The scorer and its tests move together (`benchmark/scoring/rates.py` wraps the frozen `stats.make_rate`, which is not edited because the 27 Oct 2026 freeze hashes it).
- **Fixtures.** `fixtures/{valid,invalid}/benchmark-scorecard.v3`: every v2 fixture with the member added, plus four invalid ones (missing, above one, negative, not a number). No generated models: `benchmark-*` contracts are excluded from codegen.

## Error catalogue v2 (27 Oct 2026)

`schemas/errors.v2.json` sits beside `errors.v1.json`, which is not edited (a test pins its bytes). It is v1 plus one code, `DUPLICATE_CREATE` (non-retryable, 35 codes: 5 retryable, 30 non-retryable). Decision record: `DEC-2026-10-27-record-event-v2` (D6) in `Noemai-org/docs`.

- **What it names.** A create that is conditional on absence was refused because the target already holds a case for the same subject and epoch, made under another idempotency key (mock-case: `412 PRECONDITION_FAILED`). It is not `IDEMPOTENCY_CONFLICT` (the same key, another payload), not `REJECTED_STALE_VERSION` (a version token that moved) and not the same key sent twice (that is an acknowledgement, REQ-EXE-007).
- **Where it is used.** The case adapter reports it as the transport class `DUPLICATE_CREATE`; Execution classifies that class as `REJECTED_BY_TARGET` (PERD §27.1 E10, "4xx validation/business rejection"). No execution state is added, so the SoT and `state-registry.md` are unchanged.
- **Migration note.** A larger closed enum is breaking for a strict reader of v1, hence a new major. A document is v1 or v2: v1 does not accept the new code. No producer writes the code yet, so nothing needs migrating. No generated models exist yet (CON-03).

## Qualification record and run record (6 Nov 2026)

`schemas/qualification-run.v1.json` and `schemas/qualification-record.v1.json` are new (UND-13, REQ-INT-006, ruling A17). Neither edits an existing schema. Migration note: none, since both are new contracts at major 1; see `INVENTORY.md`. The reference implementation, the CLI and the rules for the lookup are in `benchmark/qualification/`; its records live in `benchmark/qualification/records/`. Fixtures: `fixtures/{valid,invalid}/qualification-run` and `qualification-record`, generated from the reference implementation by the tests' own builders.

## Case contract v2, a draft (7 Nov 2026)

`schemas/benchmark-case.v2.json`, `benchmark-oracle.v2.json`, `benchmark-fixture.v2.json` and `benchmark-arm-case.v1.json` are drafts for the PERD §48.1 amendment (ruling A27). Nothing uses them yet and no existing schema is edited. Migration note: see `INVENTORY.md`. The arm case is the only view of a case an arm is shown. Fixtures: `fixtures/{valid,invalid}/benchmark-case.v2`, `benchmark-oracle.v2`, `benchmark-fixture.v2`, `benchmark-arm-case`.

## Neutral arm result (7 Nov 2026)

`schemas/benchmark-arm-result.v1.json` is new (BEN-16, benchmark suite definition v1). It edits no existing schema. Migration note: none, since it is a new contract at major 1; see `INVENTORY.md`. It carries the seven fields every arm must produce and no Noema internals. Fixtures: `fixtures/{valid,invalid}/benchmark-arm-result`.

## Action Graph v2 (1 Nov 2026)

`schemas/action-graph.v2.json` sits beside `action-graph.v1.json`, which is not edited (AGENT-15). Task PCK-04. Source: PERD v1.19 REQ-AG-001 as amended by `DEC-2026-10-31-proof-depth-and-completeness` D9 ("every node other than a declared root has at least one incoming edge"); the `roots` field and the connectedness rule were approved by the Architect on 1 Nov 2026 (E1, option a).

- **What v2 is.** v1 plus a required `roots` array: one or more unique node IDs, the nodes the planner may start from. Every other node needs an incoming edge.
- **What packcheck adds** (not expressible in the schema): every root names a node of the graph; every non-root node has an incoming edge; every node has at least one postcondition; every WRITE node has at least one precondition; a graph with more than one node is connected by its edges, so a flat list of endpoints fails.
- **Migration note.** A new required property is breaking for a v1 document, hence a new major. A graph is v1 or v2: a v1 graph lacks `roots` and does not validate as v2. packcheck now loads every graph as v2. The only graphs are the three fintech files, which gain `roots` in the same change; the conformance packs carry no graph. `pack.v1.json` changes only the description of `action_graph_files`, which now names v2; validation is unchanged. No generated models exist yet (CON-03); they are generated when CON-03 lands.

## Status (Session A, 24 Oct 2026)

- **Built:** every schema in `INVENTORY.md`, with a valid and an invalid fixture each, the golden vectors, and the drift checks (`tools/contracts`, `tools/check_vectors.py`). Error codes are in `schemas/errors.v1.json`.
- **Constraints:** both founders review changes (CODEOWNERS). The PERD §32.2 and state-registry files here are vendored copies; do not edit them.
- **Open:** `INVENTORY.md` is also changed by #57; the second PR to merge merges `main` in. #59 adds the pack `understanding` schema fields.
