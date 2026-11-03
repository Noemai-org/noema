# tools/codegen

Generates the Go and Pydantic types of every contract in `contracts/schemas/` (CON-03; REQ-API-001, REQ-TST-003; AGENT-15). Updated 3 Nov 2026.

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

Both are committed. A hand edit, an added file, a missing file or a stale schema makes `make codegen-check` fail (`generate.check`). Hand-written files allowed beside the generated Go: `doc.go`, `README.md` and `*_test.go`. Beside the Python: `README.md`.

Type names are the same in both languages: `<Schema><Major>` for a schema (`RecordEventV3`) and `<Schema><Major><Def>` for a definition (`RecordEventV3Emitter`); an inline object, enum or union gets a name derived from its path (`ReconstructionV3Chain`).

## What a generated type guarantees

It models **shape**: objects, arrays, maps, enums, constants, references, nullable values and unions.

- Go: on decode, an unknown member is refused where the schema is closed, a required member must be present, a member that may not be null is not null, no member repeats, and an enum or constant holds one of its values. A required array encodes as `[]`, never `null`. A member that is optional and nullable is an `Opt[T]` so absent and `null` stay distinct. A scalar union, and the constant `null`, are checked on decode and kept as the bytes received.
- Python: strict models (no coercion: `"1"` is not an integer, `1.0` is not an integer), `extra="forbid"` where the schema is closed, patterns, lengths and bounds that a type can hold. Validate JSON text (`Model.from_json`, `validator_for(T).validate_json`), not dictionaries.

It does **not** model validation: patterns and bounds in Go, and every conditional rule (`if`/`then`/`else`, `not`, `oneOf` constraints, `contains`) in both. The JSON Schema validator owns those, at every boundary.

The generator never guesses. A keyword it does not know, or a combination it does not model, is an error that names the schema and the path (`ir.GenerationError`).

## Not generated

`tools/codegen/excluded.json` is the one list, read by the generator and by both test suites.

- `benchmark-*`, `arm-manifest`, `fault-plan` (every version): owned by the benchmark runner and the synthetic environment. The core and the model service hold no benchmark, oracle or fault-plan type (INV-22, REQ-TST-006).
- Go only: `memory-observation.v1`. It has a required member `benchmark_case_id` (AGENT-23), and the architecture lint (`benchmark-branch`) refuses that term in core Go source. The Python model is generated. Open question in the pull request.
- An enum value that contains a term the lint refuses (`benchmark-runner` in `record-event.v3` `Emitter`) is carried as data in `enum_values_gen.json`, as `record/chain` does with `actors.json`.

## Tests

| Test | What it proves |
|---|---|
| `test_codegen.py` (this folder, `make codegen-check`) | refusals, shapes on small schemas, determinism, the committed output is current, a planted hand edit, extra file, missing file and forged header are each found |
| `services/core/internal/contracts/roundtrip_test.go` | every valid fixture decodes into its Go type and encodes to the same document; every invalid fixture in `shape_refusals.json` is refused |
| `services/model/tests/test_contracts_generated.py` | the same two properties for the Pydantic models; the registry covers every schema and definition |
| `consumer_test.go` and `services/model/tests/test_contracts_consumer.py` | the candidate-read contract between the model service and the core, on two recordings in `consumer/` |
| `tools/contracts/test_shape_refusals.py` | `shape_refusals.json` is current |

`shape_refusals.json` is derived, by `shape_refusals.py`, from the validator's own error paths: an invalid fixture is listed when it breaks `required`, `additionalProperties`, `enum`, `const` or `type` with no `if`/`then`/`else`/`allOf`/`oneOf`/`anyOf`/`not` above it. Nobody keeps it by hand.

`consumer/model-candidates-request.json` is written by the model service's real client (`NOEMA_UPDATE_RECORDINGS=1`); `consumer/core-candidates-response.json` by the core's Go test (`go test ./internal/contracts -update`). Each side reads the other's. Review the diff when either changes.

## Changing a contract

Edit the schema (a version bump, a migration note, AGENT-15), then `make codegen`, then `.venv-contracts/bin/python tools/codegen/shape_refusals.py --write`, and commit the result with the schema change.
