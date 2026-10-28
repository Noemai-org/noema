# execution/e2e

**What it does.** The first end-to-end case as tests: G2 escalate (graph `fin.graph.escalate`) through the core adapter against the real `mock-case` binary and real PostgreSQL. No test double stands in for the case system, the database or the controller.

**How it runs.** `go test ./execution/e2e/...` from `services/core/`. The harness builds `synthetic/mock_case` with `go build`, creates its database and roles on a throwaway server (`internal/testpg`), writes its two credentials to files, starts it on free ports and waits for `/healthz`. The core's case client gets only the runtime credential. Nothing in this package calls mock-case's truth or control endpoints: the fault-plan client belongs to the benchmark runner (DEC-2026-10-26), so no FaultPlan is applied here.

**What is real and what is a stand-in.**
- Real: `adapters/synthetic_tushe.CaseClient`, `execution/bridge`, `execution/controller`, `execution/ledger` (as the `execution_rw` role), `record/chain`, the migrations, the Temporal workflow `workflows.EscalateCase` (in the Temporal SDK's in-process test environment, not the server of the Compose stack).
- Stand-in (`stack_test.go`, `trustStandIn`): Trust's grant issuing and the plan and intent reads. It signs grants with a synthetic key and stores them as the `trust_rw` role would. It decides nothing about policy.

**Tests.**
- `TestG2EscalateThroughTheCoreAdapterAgainstMockCase`: create with the idempotency key of PERD §27.2, read back by that key with the version token, route with that token; eight Record events, chain verified, two receipts, two outbox events.
- A spent create grant executed again writes nothing; a second create for the same transaction and a route bound to a stale token are voided at E7 before any write; with the kill switch on nothing is spent and mock-case receives nothing.
- `TestG2ARouteRacedByAnotherWriterAfterRevalidationIsRefusedAndTheGrantIsVoided` (US-19, EC-E11, REQ-EXE-004): another writer routes the case after E7; the real mock-case refuses the grant's token, the receipt is `REJECTED_STALE_VERSION`, the grant is void with a RecoveryRequest, the case holds the other writer's queue, and a second execution writes nothing. The check that no write was applied reads the case through the runtime client; the harness holds no handle on mock-case's truth API.

**Not covered here (and why).** A fault between E7 and E9 (the concurrent-modification and dropped-response cases): they need the fault-plan client, which is not in the core. The adapter's contract tests replay recordings made with real FaultPlans (`adapters/synthetic_tushe/testdata`).
