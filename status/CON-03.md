# Status · CON-03 (Core Engineer 1) · codegen to Go and Pydantic

Updated 2026-11-03. Plan task CON-03: owner C1+AG; REQ-API-001, REQ-TST-003; Needs CON-01; done when "Regeneration gives no diff; contract tests run in CI". Unblocks ENV-14 (G0-N), UND-02, UND-04, TRU-01, EXE-03, CON-04, CON-06, ENV-08, UI-01.

## State: in review (draft until checks are green at the final head and Codex has reviewed)

Generator B (our own, `tools/codegen/`, standard library only), decided under standing order 6 of the Founder: inside the task, reversible, no new dependency. Details: `tools/codegen/README.md`.

## Hand-written types that duplicate a contract (not moved by this task; each owner moves their own, on a task card)

Found mechanically (member-name overlap of every hand-written Go struct and Python class with every contract object) and by listing the code that names a schema. "Mirror" means the code restates the contract and a test or comment keeps the two together; moving it onto the generated type removes the restatement.

| Where | Contract | Kind | Owner |
|---|---|---|---|
| `services/core/execution/controller/types.go:7` `Grant` | `decision-grant.v1` (20 of 22 members) | struct | Engineering Lead (Trust) and C1 (Execution reads it) |
| `services/core/execution/controller/types.go:31` `TargetVersion` | `decision-grant.v1` `target_versions` item | struct | same |
| `services/core/execution/controller/controller.go:256` `Receipt.Document()` | `execution-receipt.v1` | dictionary builder, signed bytes | C1 (EXE-07 changes the signing, so it moves with it) |
| `services/core/record/chain/types.go`, `chain.go` | `record-event.v3` (event types, emitters, principal kinds, void reasons, patterns) | hand-kept sets, a test compares them with the schema | C1 (REC-08 changes the event) |
| `services/core/record/reconstruct/sets.go`, `project.go` | `enums.v1` (decision outcome, execution state, verdict, fact status) and `reconstruction.v3` facts | hand-kept sets, a test compares them | C1 (REC-04) |
| `services/core/adapters/synthetic_tushe/case_client.go` | `errors.v2` codes | constants | Integration Lead |
| `services/core/trust/decide/clarify.go:48` `Clarification` | `clarification.v1` (5 of 11 members) | partial struct | Engineering Lead (TRU) |
| `services/model/noema_model/evidence/client.py` `build_request`, `parse_response`, the regexes and constants | `candidates-request.v1`, `candidates-response.v1`, `money.v1` | hand-written client, a test runs every fixture through it | Core Engineer 2 |
| `services/model/noema_model/understanding/resolution/types.py:40` `Candidate` | `candidates-response.v1` candidate (9 of 9) | domain dataclass | Core Engineer 2 |
| `services/model/noema_model/understanding/pipeline/catalogue.py:40` `SlotInfo` | `intent-node.v1` slots item (5 of 7) | dataclass | Core Engineer 2 |
| `services/model/noema_model/understanding/pipeline/types.py:47` `PipelineConfig`, `resolution/config.py:35` `RankingConfig` | `pack.v1` understanding and ranking (7 of 10, 5 of 8) | configuration dataclasses | Core Engineer 2 |
| `services/model/noema_model/harness/errors.py` | `errors.v1` codes | constants | Core Engineer 2 |

**A divergence the generated types exposed.** The model service's client accepts any three-letter currency (`^[A-Z]{3}$`); `money.v1` `CurrencyCode` allows `NGN` only (`contracts/currencies.json`). One of them is wrong. Owner: Core Engineer 2 (the client) with the contract owners.

## Open question for the founders (lint versus a contract member)

`memory-observation.v1` has a required member `benchmark_case_id` (AGENT-23). The `benchmark-branch` lint refuses that term in core Go source, so the Go type is held back (`excluded.json`, `go_only`) and MEM-03 and MEM-04 will need it. Options: (1) exempt `internal/contracts/*_gen.go` from `benchmark-branch`, because generated mirrors hold no logic (recommended; `tools/lint` is a protected path and a safeguard, so it needs a founder); (2) rename the member in a `memory-observation.v2` (a contract change); (3) carry such members as data like the enum values (more machinery). Decide before MEM-03 starts.

## Session 2026-11-03 · CON-03

### Session summary
- What changed (PR links): the CON-03 pull request on `feat/CON-03-codegen`.
- Why: REQ-API-001, REQ-TST-003; on the critical path of ENV-14 (G0-N).
- Risks, and how they are contained: the generated Go decodes strictly where Go lets us and leaves patterns, bounds and conditional rules to the validator (documented, and each language's tests measure it against the invalid fixtures); `.github/workflows/safeguards.yml` and the Makefile are changed as the Architect approved.
- Tests added (REQ-API-001, REQ-TST-003): `tools/codegen/test_codegen.py`, `tools/contracts/test_shape_refusals.py`, `services/core/internal/contracts/{roundtrip,consumer}_test.go`, `services/model/tests/test_contracts_{generated,consumer}.py`.
- Regression cases added: the four shape gaps the first run found (a scalar union, a null-only type, an integer written as a number, an enum with null) and the missing `TypeAlias` import are each covered by the fixtures that exposed them.
- Debt entries: none filed. Proposed: the duplicates table above.
- Conflicts raised: none. One open question (above).

### Pre-work for the next agent
- Exact next step: Codex review, then the founders; owners move their duplicates (table above).
- Do not touch: noema#94, noema#90 (held, DEBT-01).

## Session 2026-11-11 08:30 UTC · whole-string patterns in the Pydantic models and the contract validator (A138)

## Session summary
- What changed: `tools/codegen/anchors.py` (`fully_anchored`: every unescaped `^` outside a character class becomes `\A`, every unescaped `$` becomes `\Z`), used by the Pydantic generator, so a generated `pattern=` is the whole-string form; `tools/contracts/validate.py` applies the same rule to the `pattern` keyword. Models regenerated (68 patterns), shape-refusals regenerated.
- Why: the Architect's A138 (instruction of 11 Nov; not yet in docs/decisions, see PR). In Python `re`, `$` also matches before a final newline, so a generated model accepted `raw_ab\n` for `^raw_[0-9A-Za-z]{2,32}$`; JSON Schema's ECMA 262 `$` does not. Found while checking bounds on noema#409.
- Risks, and how they are contained: a pattern whose body admits a newline (`[\s\S]*`) still does; every valid fixture still validates (model suite 794 passed; `validate.py` all pass); the Go types are unchanged (they do not model patterns, see tools/codegen/README.md).
- Tests added: `services/model/tests/test_contract_patterns.py` (4 tests: the generated set is exactly the `\A...\Z` form of the schema patterns, no bare anchor remains, and for a witness taken from the valid fixtures the newline answer equals `re.fullmatch` of the core); `tools/codegen/test_codegen.py` class `Anchors` (5 tests); invalid fixture `memory-observation.v2/memory_id_trailing_newline.json` (pattern). The first two commits are tests only; their red state was seen locally, not in CI.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none for this change. Go does not check contract patterns at all (`tools/codegen/README.md`); a Go-side pattern validator at the core's boundaries is a separate decision for the Architect.
- Do not touch: generated files by hand.
