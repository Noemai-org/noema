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

- **What v3 is.** v2 with the emitter, the principal and `payload_hash` defined. `schema_version` is major 3. `written_by` is one of 17 service identities (the ten of the PERD §41.1 table and the seven §34.1 logical services whose adapters carry an identity; `fault-controller` and `fixture-loader` are benchmark-only and not emitters). `actor` is required and is `<kind>:<id>` (kind: customer, operator, approver, auditor, regulator, admin; id the session token's `sub`, no colon) or, when no authenticated person caused the event, the emitter's own identity, equal to `written_by` (a rule per emitter in the schema). `service` is not a principal kind. `payload_hash` is SHA-256 of the UTF-8 RFC 8785 form of the payload, integers only; an event that refers to no object has the payload `{}`. The event types, security kinds, nine grant-void reasons and failover reasons are those of v2.
- **Migration note.** `actor` changes meaning and form, so v3 is not additive (new major, v2 unchanged). No v2 event exists outside tests and fixtures, so nothing is migrated: the writer (`services/core/record/chain`), the verifier, the ledger (emitter `core-execution`) and the benchmark runner's reader (`reconstruction.v3`) move together. No generated models exist yet (CON-03).
- **Generated files.** `python3 -I tools/record_chain_ref_v3.py --write` writes the v3 fixtures and the `record_event_chain_vectors_v3` family; `--check` (also run by `tools/contracts/test_record_event_v3.py`) fails on any difference. v2 is still written by `tools/record_chain_ref.py`, and its output did not change. The `payload_hash_vectors` family comes from `tools/payload_hash_independent.js` (Node), not from the generator.
- **Tooling.** `tools/jcs_ref.py` is the Python RFC 8785 for the generators: it sorts members by UTF-16 code unit and refuses any number that is not an integer within 2^53-1.

## Error catalogue v2 (27 Oct 2026)

`schemas/errors.v2.json` sits beside `errors.v1.json`, which is not edited (a test pins its bytes). It is v1 plus one code, `DUPLICATE_CREATE` (non-retryable, 35 codes: 5 retryable, 30 non-retryable). Decision record: `DEC-2026-10-27-record-event-v2` (D6) in `Noemai-org/docs`.

- **What it names.** A create that is conditional on absence was refused because the target already holds a case for the same subject and epoch, made under another idempotency key (mock-case: `412 PRECONDITION_FAILED`). It is not `IDEMPOTENCY_CONFLICT` (the same key, another payload), not `REJECTED_STALE_VERSION` (a version token that moved) and not the same key sent twice (that is an acknowledgement, REQ-EXE-007).
- **Where it is used.** The case adapter reports it as the transport class `DUPLICATE_CREATE`; Execution classifies that class as `REJECTED_BY_TARGET` (PERD §27.1 E10, "4xx validation/business rejection"). No execution state is added, so the SoT and `state-registry.md` are unchanged.
- **Migration note.** A larger closed enum is breaking for a strict reader of v1, hence a new major. A document is v1 or v2: v1 does not accept the new code. No producer writes the code yet, so nothing needs migrating. No generated models exist yet (CON-03).

## Status (Session A, 24 Oct 2026)

- **Built:** every schema in `INVENTORY.md`, with a valid and an invalid fixture each, the golden vectors, and the drift checks (`tools/contracts`, `tools/check_vectors.py`). Error codes are in `schemas/errors.v1.json`.
- **Constraints:** both founders review changes (CODEOWNERS). The PERD §32.2 and state-registry files here are vendored copies; do not edit them.
- **Open:** `INVENTORY.md` is also changed by #57; the second PR to merge merges `main` in. #59 adds the pack `understanding` schema fields.
