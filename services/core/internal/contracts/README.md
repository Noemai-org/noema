# internal/contracts

**What it does.** Generated Go types for the contracts in `contracts/schemas/`, committed. Updated 3 Nov 2026.

**REQs.** REQ-API-001, REQ-TST-003.

**Status.** Built (CON-03). 49 files: one `*_gen.go` per schema, `runtime_gen.go`, `registry_gen.go` and `enum_values_gen.json`. How they are made, what a type guarantees and what it leaves to the validator: [`tools/codegen/README.md`](../../../../tools/codegen/README.md). Regenerate with `make codegen` from the repository root.

**Not here.** Benchmark, oracle and fault-plan contracts, and `memory-observation.v1` (see the codegen README for why).

**Tests.** `go test ./internal/contracts/...` from `services/core/`: every valid fixture round-trips, every shape violation is refused, and the candidate-read recordings of the model-service contract are read. Hand-written files allowed here: `doc.go`, `README.md`, `*_test.go`.
