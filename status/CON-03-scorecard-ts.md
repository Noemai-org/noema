# Status · CON-03 (Core Engineer 1) · benchmark TypeScript entry point for scorecard-view.v1 (noema#677)

Updated 2026-11-17. Plan task CON-03 (codegen); REQ-UI-004, REQ-BEN-004, REQ-API-001, INV-22; A250, A258. Prepared locally on `prep/ts-scorecard`; not pushed, no PR opened.

## State: prepared locally (two commits), waiting for a push and review

## Messages

| Direction | Peer | Purpose | What the receiver used it for |
|---|---|---|---|
| received | Frontend Engineer, noema#677 | asks for a generated, benchmark-only TypeScript parser for `scorecard-view.v1` (merged in noema#642), isolated from the customer and operator entry points, with valid and invalid fixtures and no-case-oracle-reachability tests; says UI-05's Done condition also needs BEN-16's latest real run, which fixture rendering is not | the task of this session. The BEN-16 point is not part of this change; it stays with UI-05 and BEN-16 |

# Session 2026-11-17 18:48 WAT · CON-03 (noema#677)

## Session summary
- What changed (no PR yet): `tools/codegen/ts.json` has a third list, `benchmark` (`scorecard-view.v1.json`); `tools/codegen/generate.py` (`render_ts`) reads it and writes a third entry point, `ui/src/contract_types/benchmark.ts`, beside `customer.ts` and `operator.ts` (named after the audience, as they are). `operator.ts` does not include it. Generated, not hand-edited: `benchmark.ts`, `scorecard_view_v1.ts`. READMEs: `tools/codegen/README.md`, `ui/src/contract_types/README.md`, `ui/bench/README.md`.
- Why: noema#677. The generator knew only two lists; `scorecard-view.v1` had Go and Pydantic types (#642) and no TypeScript parser. No schema change, no fixture change.
- Risks, and how they are contained: the benchmark list is optional in `ts.json` (`cfg.get`), so the existing generator tests that write a two-list `ts.json` are unchanged. The view refers to no other contract, so the closure of `benchmark.ts` is the view and the runtime; the isolation tests fail if it grows or if another entry point reaches it (two mutants run: the view added to the customer list, and `customer-answer.v1` added to the benchmark list; each failed four tests).
- Tests added (REQ-UI-004, REQ-BEN-004, INV-22, A250, A258): `tools/codegen/test_codegen.py`: `test_the_benchmark_entry_point_holds_the_scorecard_view_and_only_that`, `test_the_three_entry_points_do_not_leak_into_each_other`, `test_the_benchmark_entry_point_reaches_no_case_id_oracle_or_write_access`; `test_the_front_end_bundle_holds_only_what_a_root_reaches` now also takes the roots of `benchmark.ts`. `ui/test/benchmark_contracts.test.mjs`: valid fixtures parse; every invalid fixture of the view is refused; import isolation; planting case, oracle, tenant and write-role members in five places is refused; no member of a parsed view names a case, oracle, tenant or write access; the entry point imports only the runtime and the view and has no network call.
- Failing first: commit `dd6c7af3` (red: three Python tests failed, the UI file failed to load `benchmark.js`); the change is the next commit.
- Regression cases added: the above.
- Debt entries: none. Conflicts raised: none.
- Observed, not changed: the type-name scan reads names, not enum values or doc comments (the schema's Tier 0 gate enum holds `ORACLE_ACCESS`, `WRITE_OUTSIDE_REGISTRY`, `WRITE_WITHOUT_VALID_GRANT`, and the doc comments say what the view leaves out). `layers` holds a count under the key `HELD_OUT` in the valid fixtures: a number of cases per layer, no reference; the Benchmark Owner owns whether a layer count of the held-out set may be shown (INV-22). Not an issue of the parser.
- Not affected by noema#682 (the generated parser does not enforce `prefixItems` and `allOf` conditional constraints): `scorecard-view.v1` uses neither (only `oneOf` in `PassK`, which the parser holds), and every invalid fixture of the view is refused by the parser.

## Pre-work for the next agent
- Exact next step: push, open the PR (task card, plan, SoT §52 questions, PR checklist; P0 path: contracts), then the Frontend Engineer imports `parseScorecardViewV1` from `ui/src/contract_types/benchmark.ts` in `ui/bench`.
- Open questions (owner): the `HELD_OUT` layer count above (Benchmark Owner); UI-05's Done needs BEN-16's real run (Frontend Engineer, Benchmark Owner).
- Files to read first: `tools/codegen/generate.py` (`render_ts`), `tools/codegen/test_codegen.py` (`TypeScriptOutput`), `ui/test/benchmark_contracts.test.mjs`.
- Do not touch: `ui/src/contract_types/**` by hand.
