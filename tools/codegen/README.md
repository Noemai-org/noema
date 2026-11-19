# tools/codegen

Generates the Go and Pydantic types of every contract in `contracts/schemas/` (CON-03; REQ-API-001, REQ-TST-003; AGENT-15). Updated 3 Nov 2026; the TypeScript conditional rules 17 Nov 2026 (noema#682).

```
make codegen         rewrite the committed types from the schemas
make codegen-check   what CI runs: fail on any difference, then run this folder's tests
```

The generator is Python standard library only and needs `gofmt` (Go toolchain) to format what it writes. It adds no dependency (AGENT-14).

## What is generated, and where

| Output | Path | Notes |
|---|---|---|
| Go types | `services/core/internal/contracts/*_gen.go` | one file per schema, `runtime_gen.go`, `registry_gen.go`, `enum_values_gen.json` |
| Pydantic v2 models | `services/model/noema_model/contracts/*.py` | one module per schema, `_base.py`, `registry.py`, `__init__.py` |
| TypeScript types and parsers | `ui/src/contract_types/*.ts` | only the contracts named as roots in `tools/codegen/ts.json` (the ones the UI reads, REQ-UI-001) and what they refer to; a contract in `excluded.json` is never a root and never reached (the benchmark scorecard refers to the benchmark case, so it stays out, INV-22; its approved projection `scorecard-view.v1`, A250, is not excluded and is the one root of the `benchmark` list). Three entry points are written, one per audience, from the lists `customer`, `operator` and `benchmark` of `ts.json`: `customer.ts`, `operator.ts` (the customer and operator roots) and `benchmark.ts` (`scorecard-view.v1` only, for the benchmark UI, REQ-UI-004). The benchmark list is its own: no entry point reaches another's contracts. Only the named types a root uses, directly or through another, are emitted: a shared file such as `enums.v1` is read in part, and an unused definition (the core reason codes) does not reach the client bundle. A parser also holds what a type system cannot: patterns (whole-string), string lengths in code points, number and list bounds, `oneOf` (exactly one alternative: `union<T>`), a map's key pattern (`propertyNames.pattern`), and the conditional rules a type cannot carry, written as checks (see below). |

Both are committed. A hand edit, an added file, a missing file or a stale schema makes `make codegen-check` fail (`generate.check`). Hand-written files allowed beside the generated Go: `doc.go`, `README.md`, `intentref.go` (the parser of `common.v1#IntentRef`, ruling A319; a function, not a contract type) and `*_test.go`. Beside the Python: `README.md`.

Type names are the same in both languages: `<Schema><Major>` for a schema (`RecordEventV3`) and `<Schema><Major><Def>` for a definition (`RecordEventV3Emitter`); an inline object, enum or union gets a name derived from its path (`ReconstructionV3Chain`).

## What a generated type guarantees

It models **shape**: objects, arrays, maps, enums, constants, references, nullable values and unions.

- Go: on decode, an unknown member is refused where the schema is closed, a required member must be present, a member that may not be null is not null, no member repeats, and an enum or constant holds one of its values. A required array encodes as `[]`, never `null`. A member that is optional and nullable is an `Opt[T]` so absent and `null` stay distinct. A scalar union, and the constant `null`, are checked on decode and kept as the bytes received.
- Python: strict models (no coercion: `"1"` is not an integer, `1.0` is not an integer), `extra="forbid"` where the schema is closed, patterns, lengths and bounds that a type can hold. Validate JSON text (`Model.from_json`, `validator_for(T).validate_json`), not dictionaries.

Patterns in the Python models are emitted whole-string anchored (`\A...\Z`, `anchors.py`; A138): Python's `$` also matches before a final newline, which JSON Schema's and Go's `$` do not, so a plain `^...$` would accept `"ab\n"`. The contract validator (`tools/contracts/validate.py`) applies the same rule to the `pattern` keyword.

It does **not** model validation: patterns and bounds in Go, and every conditional rule (`if`/`then`/`else`, `not`, `oneOf` constraints, `contains`) in Go and Python. The JSON Schema validator owns those, at every boundary. The TypeScript parsers do hold them (next section).

The generator never guesses. A keyword it does not know, or a combination it does not model, is an error that names the schema and the path (`ir.GenerationError`).

## The conditional rules of the TypeScript parsers (noema#682)

A TypeScript parser refuses what the schema refuses, not only what its shape refuses (REQ-UI-001, INV-02): the UI shows only what a contract admits. `ir.py` keeps, on every type it builds, the keywords the type's structure does not carry as `rules` (raw schema, `$ref` resolved); Go and Pydantic ignore them. `ts.py` writes each rule as a **check**, a function from a value to a problem or `null` (`_runtime.ts`), and wraps the parser in `enforce(value, p, CHECK_n)`, so a rule is held after the shape.

| Keyword (JSON Schema 2020-12) | Check |
|---|---|
| `prefixItems`, `items` after a prefix | `cPrefix`, `cItems` (the order of the twelve sections of `reconstruction.v3`) |
| `allOf`, `anyOf`, `oneOf` over constraints | `cAll`, `cAny`, `cOne` |
| `if` / `then` / `else` | `cIf` (the facts of a section kind, `errors` retryable, `record-event` emitters) |
| `not`, `contains`, `propertyNames` | `cNot`, `cContains`, `cNames` |
| `properties`, `required`, `type`, `const`, `enum`, `$ref` | `cProps`, `cRequired`, `cType`, `cConst`, `cEnum`, `cRef` (a `$ref` parses as the named type, so a conditional may name an enum of another contract) |
| `pattern`, `minLength`, `maxLength`; `minimum`, `maximum`, `exclusiveMinimum`; `minItems`, `maxItems`; `minProperties`, `maxProperties` | `cStr`, `cNum`, `cCount`, `cSize` |

Like JSON Schema, each keyword applies only to a value of its own type, and `if` holds when its schema passes (`properties` over a missing member passes). **A keyword the emitter does not implement stops generation** with the schema and the path (`uniqueItems`, `format`, `minContains`, `additionalProperties` inside a rule, `then` without `if`, any keyword it does not know): no keyword of a contract is dropped without anyone knowing. To use a new keyword, implement it in `ts.py` (and its check in `RUNTIME`) with a test, or take it out of the contract by a version bump (AGENT-15). A definition that only a rule names (`DecisionOutcome`, for the `DECISION` section) is generated with the parsers that reach it, and no more.

## Not generated

`tools/codegen/excluded.json` is the one list, read by the generator and by both test suites.

- `benchmark-*`, `arm-manifest`, `fault-plan` (every version): owned by the benchmark runner and the synthetic environment. The core and the model service hold no benchmark, oracle or fault-plan type (INV-22, REQ-TST-006).
- Go only: `memory-observation.v1` is not generated. It has a required member `benchmark_case_id` (AGENT-23), and the architecture lint (`benchmark-branch`) refuses that term in core Go source. It is superseded by `memory-observation.v2` (Founder decision D10, 7 Nov 2026), whose member is `provenance_ref` and which is generated for Go and Python. The Python model of v1 is generated.
- An enum value that contains a term the lint refuses (`benchmark-runner` in `record-event.v3` `Emitter`) is carried as data in `enum_values_gen.json`, as `record/chain` does with `actors.json`.

## Tests

| Test | What it proves |
|---|---|
| `test_codegen.py` (this folder, `make codegen-check`) | refusals, shapes on small schemas, determinism, the committed output is current, a planted hand edit, extra file, missing file and forged header are each found |
| `services/core/internal/contracts/roundtrip_test.go` | every valid fixture decodes into its Go type and encodes to the same document; every invalid fixture in `shape_refusals.json` is refused |
| `services/model/tests/test_contracts_generated.py` | the same two properties for the Pydantic models; the registry covers every schema and definition |
| `consumer_test.go` and `services/model/tests/test_contracts_consumer.py` | the candidate-read contract between the model service and the core, on two recordings in `consumer/` |
| `tools/contracts/test_shape_refusals.py` | `shape_refusals.json` is current |
| `test_codegen.py` `TypeScriptConditionals` | no keyword the IR reads is silently ignored by the TypeScript emitter (it writes a check or raises), a keyword it does not implement stops generation, also in a real contract, and `prefixItems` and `if` are written as checks |
| `ui/test/contracts.test.mjs` | every valid fixture of every root parses and every invalid fixture is refused (one exception: a float `seq` that `JSON.parse` reads as an integer), the eight `reconstruction.v3` fixtures of noema#682, one case per section kind, every swap of two sections |

`shape_refusals.json` is derived, by `shape_refusals.py`, from the validator's own error paths: an invalid fixture is listed when it breaks `required`, `additionalProperties`, `enum`, `const` or `type` with no `if`/`then`/`else`/`allOf`/`oneOf`/`anyOf`/`not` above it. Nobody keeps it by hand.

`consumer/model-candidates-request.json` is written by the model service's real client (`NOEMA_UPDATE_RECORDINGS=1`); `consumer/core-candidates-response.json` by the core's Go test (`go test ./internal/contracts -update`). Each side reads the other's. Review the diff when either changes.

## Changing a contract

Edit the schema (a version bump, a migration note, AGENT-15), then `make codegen`, then `.venv-contracts/bin/python tools/codegen/shape_refusals.py --write`, and commit the result with the schema change.
