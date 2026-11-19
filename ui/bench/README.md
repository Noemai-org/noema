# ui/bench

Will hold the benchmark view (REQ-UI-004). Thin: no business logic, only state the backend has verified.

Created by T1 of DEC-2026-10-20. Empty: nothing is built here yet.

The contract it reads: `scorecard-view.v1`, through the generated entry point `ui/src/contract_types/benchmark.ts` (`parseScorecardViewV1`, `PARSERS["scorecard-view.v1"]`; noema#677). Import from `benchmark.ts` only here, never from `customer.ts` or `operator.ts`, and never a hand-written type of the view.
