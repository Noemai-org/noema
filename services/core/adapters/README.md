# adapters

**What it does.** Enterprise adapters: one Go interface per logical service, so the core powers depend on interfaces only and an adapter can be replaced by a stub (REQ-ENV-001, REQ-NFR-006). The POC target is the synthetic enterprise (Tushe, plus `mock-case`).

**REQs.** REQ-ENV-001, REQ-NFR-006, REQ-EXE-007, REQ-CAP-002, REQ-ENV-002.

## What is built (ENV-03, first slice, 25 Oct 2026)

- `case.go`: the interface `adapters.Case` for `mock-case` (create with idempotency key, route with expected version, get, find) and the shared vocabulary: `WriteResult`, `TransportClass` (same strings as `execution/controller`; a test there keeps them identical), `CaseRecord`, `CaseQuery`, `CasePage`, `ReadError`.
- `synthetic_tushe/case_client.go`: `CaseClient`, the implementation (see that folder's README).

## What is not built, and why

- Interfaces for `mock-ledger`, `mock-switch`, `mock-destination`, `mock-customer`, `mock-policy`: their payload fields are "fixed when the Tushe OpenAPI is frozen" (PERD §34.4) and the Tushe-dependent work is paused until the Founder confirms. Nothing is guessed.
- `mock-clock`, `fault-controller`, `fixture-loader`: the control plane is benchmark-only and "never reachable by the runtime" (PERD §34.4, REQ-ENV-002). An interface for them in the core's adapters package would give the runtime a way to reach them. They need a decision on where they live (the benchmark side) before any code exists.
- Complaint and evidence-attach operations of `mock-case`: next slice.

## Constraints

- No method can move money, close or resolve a case, change a priority, an SLA or a clock, or reach the truth or control listeners (BD-08, REQ-ENV-002).
- May import `platform/*` and `internal/contracts` only; imported only by `execution` and `evidence` (SoT §51). No provider, no model call.
- Standard library only: this slice adds no dependency.

## Open

- A live run of the contract tests in CI: the tests replay recordings of the real mock-case. A live job needs a workflow change (a protected path).
- The Execution bridge that turns a `WriteResult` into a controller `TransportResult` and the security event for `KeyCollision` (EXE tasks).

**Tests.** `go test ./adapters/...` from `services/core/`.
