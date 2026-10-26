# Status · ENV-03 (Session A) · Go adapter interfaces

Updated 2026-10-25. Plan task ENV-03 (owner IL+AG; REQ-ENV-001, REQ-NFR-006). This file covers the first slice only.

## State

- Built (PR for this file): `adapters.Case`, the interface of `mock-case`, with the shared write and read vocabulary (`services/core/adapters/case.go`), and `CaseClient`, its implementation (`adapters/synthetic_tushe/case_client.go`).
- Evidence: contract tests replay 19 exchanges recorded from the real mock-case (real PostgreSQL, faults from real FaultPlans); a throwaway live driver ran the adapter against the real mock-case locally (all scenarios as expected; the 4 cases that mock-case committed are the 4 that should be: the 201, the 500 after commit, the dropped response and the timeout after commit); seven deliberate breaks of the adapter were all caught by the tests.
- Standard library only. No dependency was added: the Postgres driver and the Temporal SDK (approved by the Founder on 25 Oct 2026 in a working session) are not used by this slice.

## Not built, and why

- Interfaces for `mock-ledger`, `mock-switch`, `mock-destination`, `mock-customer`, `mock-policy`: PERD §34.4 fixes their field names only when the Tushe OpenAPI is frozen, and the Founder paused Tushe-dependent work on 25 Oct 2026. Nothing guessed.
- `mock-clock`, `fault-controller`, `fixture-loader`: decided by the Founder on 26 Oct 2026 (the PERD amendment is in a docs pull request, not yet merged). `fault-controller` and `fixture-loader` are benchmark-only and never reachable from core or model; their clients go under `benchmark/runner/` and `benchmark/fixtures/`, never in core. A read-only `mock-clock` client in core adapters is added only when a slice needs it (PERD §64.2 lists clock clients); this slice does not.
- Complaints and evidence-attach operations of `mock-case`: next slice.
- `ENV-01` (a Needs task): its board status is not visible to this session; the Tushe-dependent part of ENV-03 needs it.

## Open

- A live CI run against a running mock-case (needs a workflow change: protected path, founder approval).
- The Execution bridge from `WriteResult` to the controller's `TransportResult`, and the security event for `KeyCollision` (EC-E06).
- The first end-to-end case (G2 escalate against mock-case) also needs the Postgres ledger and the Temporal workflow (PLT-06, PLT-07 and the EXE tasks); not started.
- Debt: transport faults are exercised by replaying recordings and by local servers that drop or stall a connection; the live run with `DROP_RESPONSE` and `TIMEOUT_AFTER_COMMIT` from a FaultPlan was done locally only. The debt entry belongs in `docs/poc/execution/debt.md` (docs PR).

## Messages

None sent or accepted for this task. (Founder instructions are not cross-session messages and are not logged here.)
