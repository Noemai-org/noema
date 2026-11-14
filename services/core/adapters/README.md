# adapters

**What it does.** Enterprise adapters: one Go interface per logical service, so the core powers depend on interfaces only and an adapter can be replaced by a stub (REQ-ENV-001, REQ-NFR-006). The POC target is the synthetic enterprise (Tushe, plus `mock-case`).

**REQs.** REQ-ENV-001, REQ-NFR-006, REQ-EXE-007, REQ-CAP-002, REQ-ENV-002.

## What is built (ENV-03, first slice, 25 Oct 2026)

- `case.go`: the interface `adapters.Case` for `mock-case` (create with idempotency key, route with expected version, get, find) and the shared vocabulary: `WriteResult`, `TransportClass` (same strings as `execution/controller`; a test there keeps them identical), `CaseRecord`, `CaseQuery`, `CasePage`, `ReadError`.
- `synthetic_tushe/case_client.go`: `CaseClient`, the implementation (see that folder's README).
- `fin.go`: the read ports of the Tushe-dependent services (`Transactions`, `Ledger`, `Switch`, `Destination`) and the normalised `fin.*` payloads of PERD 34.4 [REC] (ENV-04 slice 1, 6 Nov 2026). Every read takes a `Scope` (tenant and authenticated principal, INV-12). `Validate` refuses a missing field or a value the port does not know as `MALFORMED`; `Unavailable` is a real answer, never an empty one; `CandidatePage.ProvesAbsence` is true only for an empty page marked complete. The field names are the stand-in's reading, not Tushe's; no implementation of these ports talks to Tushe, and no route is mapped (DEC-2026-11-06 A10).

## What is not built, and why

- Interfaces for `mock-customer` and `mock-policy`, and the beneficiary and principal-context reads (PERD 34.4 fixes no normalised shape for them): their payload fields are "fixed when the Tushe OpenAPI is frozen" (PERD §34.4) and the Tushe-dependent work is paused until the Founder confirms. Nothing is guessed.
- `fault-controller` and `fixture-loader`: benchmark-only, never reachable from core or model (PERD §34.2, §34.4, REQ-ENV-002; amendment to §34.1 in a docs pull request). Their clients go under `benchmark/runner/` and `benchmark/fixtures/`, never here.
- `mock-clock`: a read-only client is added here only when a slice needs it (PERD §64.2 lists clock clients); clock control (`simulate/advance`) stays benchmark-only.
- Complaint and evidence-attach operations of `mock-case`: next slice.

## Constraints

- No method can move money, close or resolve a case, change a priority, an SLA or a clock, or reach the truth or control listeners (BD-08, REQ-ENV-002).
- May import `platform/*` and `internal/contracts` only; imported only by `execution` and `evidence` (SoT §51). No provider, no model call.
- Standard library only: this slice adds no dependency.

## Open

- A live run of the contract tests in CI: the tests replay recordings of the real mock-case. A live job needs a workflow change (a protected path).
- The Execution bridge that turns a `WriteResult` into a controller `TransportResult` and the security event for `KeyCollision` (EXE tasks).

**Tests.** `go test ./adapters/...` from `services/core/`.

## Conditional complaint log (A185, noema#522; mock-case noema#537)

`LogComplaintInput.IfNone` makes the log conditional on the case having no complaint yet (the precondition of `log_complaint_reference`, "no complaint for the subject and epoch"; a case is the subject and epoch). The case client then sends `If-None-Match: *`, and a `412 PRECONDITION_FAILED` answer is a refused create-if-absent: `ClassDuplicateCreate` with `ExistingComplaintRef` (the complaint the target holds, to read back; never this write's result, and a create has no version token). Any other 412 body is a plain rejection. Without `IfNone` nothing changes. Tests: `synthetic_tushe/complaint_conditional_test.go` (the documented answer shape, no database) and `outcome_e2e/complaint_conditional_test.go` (the real mock-case: one acknowledgement, a named refusal, a replay, and eight concurrent logs giving exactly one; CI). The bridge that maps the capability and carries the condition is C1's (noema#522).

