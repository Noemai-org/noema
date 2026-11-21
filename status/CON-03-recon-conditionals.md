# Status · CON-03 follow-up · the conditional rules of the generated TypeScript parsers (noema#682)

Updated 2026-11-17. Core Engineer 1. Task: CON-03 (codegen), follow-up to noema#615 (TypeScript output, merged) for noema#682, raised by the Frontend Engineer. REQ-UI-001, REQ-UI-003, REQ-API-001, REQ-AUD-003, INV-02, AGENT-15.

## State: prepared locally, not pushed, no PR

Branch `prep/ts-recon-cond` from `origin/main` 91870528. Two local commits: the red tests, then the change. Nothing was pushed, no PR was opened and nothing was posted to GitHub.

## The defect

`parseReconstructionV3` (`ui/src/contract_types/reconstruction_v3.ts`, emitted by `tools/codegen/ts.py`) accepted eight committed invalid fixtures (`sections_out_of_order`, `decision_outcome_unknown_value`, `execution_state_succeeded`, `action_without_grant_state`, `grant_state_unknown_value`, `claim_without_bound_status`, `claim_status_not_a_fact_status`, `claim_text_empty`). The emitter wrote the shape and the bounds a type can hold and dropped every other keyword (`prefixItems`, `allOf`, `if`/`then`/`else`, `not`, `contains`, `anyOf`, `maxProperties`), because `ir.py` listed them as "validation" and nobody read them. The same silence let 27 more invalid fixtures of `clarification.v1`, `errors.v2`, `noema-resolution.v1` and `record-event.v3` through (the old test skipped them by name).

## What changed

- `tools/codegen/ir.py`: every type keeps, as `rules`, the keywords of its schema node that its structure does not carry (raw schema, `$ref` resolved). Not part of equality. Go and Pydantic ignore them; their output is byte-identical (test `test_go_and_python_output_does_not_depend_on_the_rules`).
- `tools/codegen/ts.py`: each rule is compiled to a check (`cPrefix`, `cItems`, `cAll`, `cAny`, `cOne`, `cIf`, `cNot`, `cContains`, `cNames`, `cProps`, `cRequired`, `cType`, `cConst`, `cEnum`, `cRef`, `cStr`, `cNum`, `cCount`, `cSize`) and the parser is wrapped in `enforce(value, p, CHECK_n)`. A keyword it does not implement (`uniqueItems`, `format`, `minContains`, `then` without `if`, any unknown keyword in a rule) stops generation with the schema and the path. The checks are in the generated `_runtime.ts` (`RUNTIME` in `ts.py`), standard library only, no dependency.
- `ui/src/contract_types/*`: regenerated, not edited.
- Docs: `tools/codegen/README.md`, `ui/src/contract_types/README.md`.

## Constructs the schemas use, and which are enforced now

`reconstruction.v3`: `prefixItems` + `items: false` (section order), `allOf` of `if`/`then` (seven section kinds), `$ref` and `oneOf` with null inside a `then`, `maxProperties`, and bounds under a `then`. Others reached by the same roots: `if`/`then`/`else` (`errors.v2`, `errors.v3`, `clarification.v1`, `noema-resolution.v1`, `record-event.v3`), `not` and `contains` and `anyOf` over constraints (`noema-resolution.v1`), `required` and `properties` as rules. All are enforced.

## Review triggers

- `tools/codegen/ts.py` is over 400 lines (604): about 150 of them are the TypeScript runtime template, which is generated into `_runtime.ts`; the rest is the compile of rules. One file, one responsibility (the TypeScript emitter); splitting it is its own refactor.
- Output outside `reconstruction.v3` changes, because the same emitter change legitimately applies: `clarification_v1.ts`, `errors_v2.ts`, `errors_v3.ts`, `noema_resolution_v1.ts`, `record_event_v3.ts` (a `CHECK_n` constant and `enforce(...)` around the parser) and `common_v1.ts`, `enums_v1.ts` (a definition that only a rule names is now reachable: `ResourceRef`, `DecisionOutcome`, `ExecutionState`, `FactStatus`, `RetryableErrorCode`, `GrantVoidReason`, `ProviderFailoverReason`; `PATTERN_n` renumbered in `common_v1.ts`). No Go or Python file changes.
- The UI parsers now refuse documents they accepted: the 35 invalid fixtures above (8 + 27). A UI that consumed a malformed body, as the Frontend Engineer reports for #638, now gets a `ContractError`.

## Session 2026-11-17 18:00 UTC · CON-03 (noema#682)

### Session summary
- What changed (PR links): none yet (local branch `prep/ts-recon-cond`, two commits).
- Why: the operator UI needs a validation boundary for `reconstruction.v3` that refuses what the schema refuses (REQ-UI-001, REQ-AUD-003, INV-02).
- Risks, and how they are contained:
  - a rule compiled wrongly refuses a valid body (or accepts an invalid one): every valid fixture parses; every invalid fixture is refused; a differential run of 66,000 mutated documents of 11 roots against the contract validator (`tools/contracts/validate.py`, jsonschema 4.26.0) disagreed 16 times, all of them a `record-event.v3` `seq` of `1.0`, which `JSON.parse` reads as `1` (the one fixture a browser cannot see, listed in the test);
  - the double parse of each section (`cRef(parseSection)` under `prefixItems`): negligible, not measured (no P95 budget on this path).
- Tests added (REQ-UI-001, REQ-API-001, REQ-AUD-003, INV-02): `ui/test/contracts.test.mjs` (every invalid fixture of every root is refused; the eight fixtures by name; valid fixtures accepted; one case per section kind and rule; every swap of two sections); `tools/codegen/test_codegen.py` `TypeScriptConditionals` (no IR keyword silently ignored, unsupported keyword refused, also in a real contract, IR rules kept, Go and Pydantic output independent of the rules).
- Regression cases added: the same tests; the committed fixtures are the cases.
- Debt entries: none.
- Conflicts raised: none. Observation, no action taken (fixtures are not mine to change): `invalid/reconstruction.v3/sections_out_of_order.json` is invalid for two reasons (the order, and an `ACTION` item without `grant_state`), so on its own it does not prove the order check; `prefixItems` is proved by the swap test.

## Pre-work for the next agent
- Exact next step: review the two commits, push the branch, open the PR against `main` (title `CON-03: the conditional rules of the generated TypeScript parsers`), body per AGENTS.md section 11; ask the Frontend Engineer to re-run the reproduction of noema#682 on the merged head.
- Open questions (owner): none.
- Files to read first: `tools/codegen/ir.py` (`Take`, `rules_of`), `tools/codegen/ts.py` (`rules`, `check`, `keyword`, `RUNTIME`), `tools/codegen/README.md` (the TypeScript conditional rules).
- Do not touch: `contracts/schemas`, `contracts/fixtures`, the generated files by hand.
