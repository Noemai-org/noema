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

## Session 2026-11-12 03:30 UTC · regenerate `session_limits_v1.py` after #439 (restores `codegen-diff` on `main`)

## Session summary
- What changed: one generated file, `services/model/noema_model/contracts/session_limits_v1.py`: its `pack_id` pattern is now `\A[a-z][a-z0-9]*\Z`, the form noema#439 (A138) generates. One line changed; produced by `python3 -I tools/codegen/generate.py --write`, not by hand.
- Why: `session_limits.v1` (EDG-01) reached `main` before noema#439 did, with the old `^...$` pattern. After #439 merged, `main` no longer equals what the schemas generate, so `codegen-diff` fails on `main` (and on the merge ref of every open PR). Found while merging `main` into noema#411. My own sequencing miss: #439's body said open PRs that regenerate contracts would need `make codegen`; it did not check what was already on `main`.
- Risks, and how they are contained: the change only tightens one pattern (a trailing newline in `pack_id` is now refused by the model; before, it was accepted: probe in the PR); no schema change, so no AGENT-15 bump.
- Tests added: none new; `tools/codegen/test_codegen.py` and `test_contract_patterns.py` already pin the anchored form for every generated pattern.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after this merges, noema#411 merges `main` and runs `make codegen` once (its two `arm_turn` models), as every open PR that adds a contract will.
- Do not touch: generated files by hand.

# Session 2026-11-13 20:00 WAT · CON-03 (TypeScript types and parsers for the UI, EL)

## Session summary
- What changed: `tools/codegen/ts.py` (new) and `generate.py`: the generator also writes TypeScript types and runtime parsers for the contracts the UI reads, to `ui/src/contract_types/`; the roots are `tools/codegen/ts.json` (today `clarification.v1`, `confirmation-request.v1`, `errors.v2`, `noema-resolution.v1`, with every contract they refer to). `ui/test/contracts.test.mjs` runs every valid and invalid fixture of those contracts through the parsers; `test_codegen.py` gains four tests.
- Why: the UI (PERD §40.4, REQ-UI-001) shows only what the core sent and never an unmasked number, and must parse untrusted JSON; the fixed stack gives codegen to Go and Pydantic only, so there was no source for UI types and the alternative was hand-written types per language (forbidden). The approach was chosen by the user in a session; it is not authoritative until committed, so this PR stays a draft until C1 (owner of `tools/codegen`), the frontend owner (`ui/`) and the Steward ([OPEN] entry, AGENT-14: a new codegen target) agree.
- Risks, and how they are contained: (1) a new generated tree in `ui/` that the frontend owner must accept; `generate.py --check` (CI `codegen-diff`) fails on a hand edit. (2) The parsers hold shape and bounds, not conditional rules (`if`/`then`, `not`, `contains`); two fixtures are listed in the test as conditional. (3) No dependency: the generator is the standard library and the output has none.
- Tests added (REQ-UI-001, REQ-API-001, AGENT-15): 18 UI tests (fixtures of four contracts, a whole-string pattern, code points, a closed object with `__proto__`, errors that never name the value, exactly-one `oneOf`, bounded lists, required members, integers, enums); four generator tests. Red first (`npm test` failed with no contracts module). Ten runtime mutants killed (pattern, closed, required, `oneOf` uniqueness, code points, `maxLength`, `maxItems`, integer check); two survived at first and got direct tests.
- Regression cases added: the fixtures themselves.
- Debt entries: none. Conflicts raised: none; open: AGENT-14 approval for the TypeScript target.

## Pre-work for the next agent
- Exact next step: after noema#521 (the public API contracts) merges, add its four contracts to `ts.json` and regenerate (a data change); then the frontend's API client in `ui/src/api/` (fetch with the session token, idempotent POSTs, an SSE reader that parses every event and reconnects with `Last-Event-ID`).
- Open questions (owner): C1 and the frontend owner accept the target and the path; whether the parsers should also refuse conditional rules (Architect).
- Files to read first: `tools/codegen/ts.py`, `ui/src/contract_types/README.md`, `ui/test/contracts.test.mjs`.
- Do not touch: `ui/src/contract_types/**` by hand.

# Session 2026-11-17 · CON-03 the three core reason codes of A254 (noema#653)

## Session summary
- What changed: `enums.v1.json#CoreReasonCode` gains `TARGET_FROZEN`, `CLOCK_BREACHED_UNROUTED`, `DISPATCH_BUDGET_EXHAUSTED`; `state-registry.md` row, `decide.coreCodes`, regenerated Go/Pydantic/TypeScript, three valid fixtures, a migration note in `contracts/README.md` (which also states how a minor change is recorded, replacing the sentence "no mechanism for a minor version" in the note of noema#564).
- Why: A254 (interim), A244 first bullet; noema#572's remaining round needs the codes.
- Count: 20, not 21 (`DECLINED_BY_APPROVER` was already one of the 17); the number is written nowhere; the Architect confirms (record `DEC-2026-11-17-reason-codes-a254` section 3).
- Tests added (REQ-TRU-001, REQ-TRU-002, REQ-API-001): `TestTheCoreReasonCodesTrustAcceptsAreExactlyTheContractsClosedList`; seen failing first (CI red at `19832e06`). `tools/contracts/drift.py` holds the registry row equal to the enum.
- Debt entries: none. Conflicts raised: none (the count is the Steward's open question).

## Pre-work for the next agent
- Exact next step: noema#572's round uses the three codes (frozen target, breached clock, exhausted dispatches) at T4 to T15 of the intake.

# Session 2026-11-16 12:40 WAT · CON-03 (the Security Reviewer's re-read of noema#615, round 2)

## Session summary
- What changed (noema#615): `ui/test/contracts.test.mjs`: the generated parsers of `errors.v3` (message and details issue) and `customer-answer.v1` (found) are run on runs of five astral-plane digits (mathematical, Adlam, segmented), on an astral run split by the core's separators, and on four astral digits as the positive control. `tools/codegen/ts.py` and `generate.py`: only the named types a root reaches are emitted (`Ts.reachable`), so `EnumsV1CoreReasonCode` and the other unused definitions of `enums.v1`, `common.v1`, `errors.v2/v3` and `record-event.v3` are no longer in the bundle; `ui/src/contract_types/` regenerated.
- Why: the Security Reviewer's blocking finding (no test showed the browser parser refuses an astral digit run; the fixtures hold none) and note 3 (internal reason-code names in the bundle).
- Risks, and how they are contained: a parser of a definition the UI does not use is gone; `tsc` in `ui/` and the codegen tests fail if a root needs one that is missing.
- Tests added (REQ-UI-001, INV-02): 15 UI cases; `test_the_front_end_bundle_holds_only_what_a_root_reaches` (every emitted parser is a root's or is called by another; no `CoreReasonCode`). Seen failing first: the reachability test at `31a5f757` (CI `codegen-diff`; the UI job passed). The astral cases pass on the current generator by design; they were checked with a mutant (a digit class without the mathematical and Adlam ranges): nine of them fail. A pattern without the `u` flag does not load at all (a range of surrogates is out of order), so that mutant fails the whole suite.
- Regression cases added: the astral cases above.
- Debt entries: none. Conflicts raised: none.
- Not done, for the contracts' owner (Security Reviewer notes 1, 2, 4) and the Architect (the audience of `reconstruction.v3`): free-text members of `reconstruction.v3` without a digit pattern, no `maxItems` on its lists, the narrower public digit pattern.

## Pre-work for the next agent
- Exact next step: the scorecard view contract (Benchmark Owner), then a root for it.
- Files to read first: `tools/codegen/ts.py` (`reachable`), `ui/test/contracts.test.mjs`.
- Do not touch: `ui/src/contract_types/**` by hand.

# Session 2026-11-17 · CON-03 noema#615 round 3 (A258, interim): reconstruction.v3 fixed in place, two entry points, one push

## Session summary
- What changed (noema#615): (1) `main` merged (a conflict only in a generated file, regenerated). (2) Two entry points, `customer.ts` (the four public contracts and `errors.v3`) and `operator.ts` (adds `reconstruction.v3`, `record-event.v3`, the clarification, confirmation-request, resolution and `errors.v2` contracts); `index.ts` is gone; `tools/codegen/ts.json` has `customer` and `operator` lists. (3) The public digit-run pattern is now one definition, `tools/contracts/digit_run.py`, with the core's whole separator set (marks, brackets, dashes, the middle dot, NEL, vertical tab; 382 ranges), written into `customer-answer.v1`, `errors.v3`, `overlay-card.v1`. (4) `reconstruction.v3` in place: `maxItems` 1000 on `events` and `timeline`, 100 on a section's `items`, `maxProperties` 50 on `facts`, the digit-run pattern on `timeline[].text`; `record-event.v3` `reason` is `^[A-Z_]{1,64}$` (every GrantVoidReason and ProviderFailoverReason value fits; no digit can sit in it).
- Not done, for the Architect: the digit pattern on `facts` values. The core serves `resolved_target`, `disclosed_refs` and `_id` facts as written (resource references and minted IDs hold digit runs by design, A184, A242), so a blanket pattern on the facts map refuses real references (the valid fixture `full_write_case` fails). The contract cannot tell a free-text fact from a reference by value. Smallest safe default: name the free-text fact keys (`text`, `reason_codes` items) as properties with the pattern, and leave the reference and ID facts as they are. The bounds (1000, 1000, 100, 50) are my proposals; A258 gives none.
- Tests added (REQ-UI-001, REQ-SEC-003, INV-02): `test_the_customer_entry_point_reaches_no_operator_contract` (Python and UI), the widened-separator and bounds cases in `tools/contracts/test_public_api.py`. No red run: one push as A258 says; mutant: `reconstruction.v3` moved to the customer list fails the entry-point test.
- Debt entries: none. Conflicts raised: none.

# Session 2026-11-17 · CON-03 noema#615 round 4 (A272, final): the digit pattern on the free-text facts only

## Session summary
- What changed (noema#615): `reconstruction.v3` `facts` declares two properties with the digit-run pattern, `text` and `reason_codes` (each item); every other fact (references, `disclosed_refs`, resolved targets, `*_id`, minted `<prefix>_<ULID>`) keeps the open-member rule and is served as written (A193, A242). Fixtures, generated from the v1 sources by `tools/record_chain_ref_v3.py` (not hand-written): valid `id_fact_with_five_digits_in_a_row` (`object_id` holding a ULID with five digits in a row); invalid `text_fact_with_five_joined_digits` and `reason_codes_item_with_five_joined_digits`. `full_write_case` still passes.
- Generator: an object that has declared properties and typed other members used to drop the key pattern and the member types in TypeScript (`...o`); it now parses the other members as a map with the key pattern, so `fact_key_not_snake_case` is still refused.
- Free-text keys named in A272: `text`, `reason_codes`. None added.
- For the Architect, one point: A272's example "12,345" is not refused by the digit-run pattern, by design (a comma ends a run: "20,000,000" is groups of three, A194). The negative fixture uses "12 345" (five digits joined by a space). Widening the pattern to commas is a ruling, not a guess.
- Tests: the two invalid fixtures (validator, TypeScript parser) and the valid one; `test_reconstruction_v3_lists_are_bounded_and_its_text_carries_the_pattern` pins the property names and that no other alternative carries the pattern. No red run: one push as A272 says; mutant (the `text` pattern removed) makes the validator accept the negative fixture.
- Bounds (1000, 1000, 100, 50) confirmed by A272; a later change is a minor version.

# Session 2026-11-17 · CON-03 noema#664 (A278, final): the facts of reconstruction.v3 by value
- What changed: the string alternative of a fact value, and each item of an array fact, carries `digit_run.FACT_VALUE`: a typed or minted ID by shape with a letter in its body, a resource reference with a letter in its body, or a string without five digits joined by separators; under any key. A272's two named keys are gone. Fixtures (generated): six refused, four accepted (`request_id` with a 31-digit UUID, `payer_id` with a card number, `usr_88213`, `fin.account:0123456789012345`, an unlisted key with a card number in a sentence, an array item; accepted `recev_1005`, `fin.transaction:TX88213`, four digits in a text, a plain sentence). The "21" of the reason-code note and test is 20 (A280).
- Tests (REQ-UI-001, REQ-SEC-003, INV-02): `test_a_fact_value_is_an_id_or_a_reference_with_a_letter_or_a_string_without_five_joined_digits`, the schema pin, the validator on the fixtures, the UI parser on the eight. Red first at `39d7532b` (only the six refused fixtures failed, in the `contracts` job).
- Left as it was: a comma ends a run (A194), so `12,345` is accepted.
