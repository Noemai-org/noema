# Status · ENV-03 (Session A) · Go adapter interfaces

Updated 2026-10-27. Plan task ENV-03 (owner IL+AG; REQ-ENV-001, REQ-NFR-006). First slice: the case adapter (merged, noema#67). Second slice: the first end-to-end case (this file's last section).

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
- Done in the second slice (see the last section): the Execution bridge, the security event for `KeyCollision` (EC-E06), the Postgres ledger, the Temporal workflow and the first end-to-end case.
- Debt: transport faults are exercised by replaying recordings and by local servers that drop or stall a connection; the live run with `DROP_RESPONSE` and `TIMEOUT_AFTER_COMMIT` from a FaultPlan was done locally only. The debt entry belongs in `docs/poc/execution/debt.md` (docs PR).

## Messages

### 2026-10-27 (time not recorded) accepted · Founder → Session A
- Purpose: proceed with the G2 escalate case through the core adapter. Approved: the Postgres driver, the Temporal SDK and this first end-to-end case. Build the Postgres ledger, the Temporal workflow and the bridge from the adapter's write result to the controller, each step behind its own test. Report create with idempotency key, read back with version token, route with that token, the run links and anything still blocked. Do not merge.
- Used it for: the pull request that carries the section "Session 2026-10-27" below.
- Evidence: the pull request of the branch `sa/g2-escalate-case`.


# Session 2026-10-27 13:35 WAT · ENV-03

## Session summary
- What changed (PR links): the first end-to-end case, G2 escalate, in one pull request on `sa/g2-escalate-case` (five commits, one per step): the migration and role tests (`PLT-06` slice), the Record append and `Verify` (`REC-01`), the Postgres ledger and contract-shaped receipts (`EXE-02`), the case bridge (`ENV-03`), the `EscalateCase` workflow (`OUT-03` slice) and the end-to-end tests (`execution/e2e`). Dependencies added with the Founder's approval of 25 Oct 2026 (DEC-2026-10-25): `github.com/jackc/pgx/v5` v5.7.6 and `go.temporal.io/sdk` v1.37.0 (both pinned in `go.mod` and `go.sum`; the module's `go 1.24` directive is unchanged).
- Why: the Founder's instruction of 27 Oct 2026 (above).
- Risks, and how they are contained: (1) The Record follows PERD §32.1 and differs from `contracts/schemas/record-event.v1.json` (see Conflicts). (2) A test fixture stands in for Trust's grant issuing and for the plan and intent reads; no code outside tests does, and the controller's ports are unchanged. (3) The workflow ran in the Temporal SDK's in-process test environment, not against the Temporal server of the Compose stack (no Docker in the session or in the core CI job).
- Tests added (REQ, INV, US or EC IDs): `platform/db` (REQ-DATA-001: role matrix, immutability, single use), `record/chain` (REQ-AUD-001, EC-E16: golden vector from Python, tamper kinds, concurrency), `execution/ledger` (REQ-EXE-001, -002, -006, -008, EC-E06, EC-E09, EC-E16: 100 concurrent executes, crash and sweep, failed append), `execution/bridge` (REQ-EXE-003, -004, -006, -007, REQ-CAP-002), `workflows` (D8, INV-30), `execution/e2e` (the G2 case, US-19 style stale-token void, case dedupe, kill switch). Each guard was checked by deliberately breaking it; two mutants survived as equivalent (the SERIALIZABLE level and an `ON CONFLICT` target, both backed by unique constraints and row locks).
- Regression cases added: the above; no bug fix in this session.
- Debt entries (to file in `docs/poc/execution/debt.md`, docs repository): (a) `tenant_id` is on every row and every query is tenant-scoped, but a repository-layer test that proves it (REQ-SEC-004) is not written; (b) the sweeper writes a receipt for every unreceipted attempt it sees; an age threshold so that it does not race a dispatch in flight is configuration (REQ-OPS-003) and is not built; (c) the transport faults between E7 and E9 against the live mock-case (dropped response, concurrent modification) are covered by recordings made with real FaultPlans, not by a live run, because the fault-plan client lives in the benchmark runner; (d) the other PERD §38.2 tables and `db_schema.md` (generated) are not built.
- Conflicts raised: two, below.

CONFLICT DETECTED
Affected IDs: INV-11, REQ-AUD-001, AGENT-15; PERD §32.1 (REC); `contracts/schemas/record-event.v1.json`
Statements in conflict: "The genesis event of each tenant chain carries `prev_hash = SHA-256("noema:genesis:" ‖ tenant_id)`" and the PERD additions `seq`, `payload_hash`, `actor` (PERD §32.1) vs "The first event of a tenant's chain has prev_hash of 64 zeros" and `additionalProperties: false` with none of the three fields (`record-event.v1.json`)
Evidence: `record/chain/chain_test.go` (golden vector computed in Python for the PERD rule); `contracts/fixtures/valid/record-event/genesis_event.json` (64 zeros)
Proposed alternatives: (1) follow the PERD and bump the contract to v1.1 with the three fields and the genesis rule, regenerate models, update the fixture (AGENT-15); blast radius: `contracts/`, the reconstruction schema, the generated types. (2) follow the contract and drop the three fields and the salted genesis from the PERD; blast radius: PERD §32.1 and the chain verifier design.
Safer reading applied meanwhile (AGENT-20): the PERD (higher in SoT §0.4). The contract is unchanged; the extra fields are in the hashed body and the stored row, not in any served contract object.
Decision required from: founders

CONFLICT DETECTED
Affected IDs: REQ-EXE-001, REQ-EXE-003, AGENT-15; PERD §27.1; `contracts/schemas/record-event.v1.json` (`GrantVoidReason`)
Statements in conflict: PERD §27.1 and REQ-EXE-003 name nine void reasons (SIGNATURE_INVALID, EXPIRED, INTENT_SUPERSEDED, POLICY_SUPERSEDED, BINDING_MISMATCH, WRITES_DISABLED, STATE_CHANGED, REVALIDATION_UNAVAILABLE, REJECTED_STALE_VERSION) vs `GrantVoidReason` lists INTENT_SUPERSEDED, STATE_CHANGED and POLICY_SUPERSEDED only
Evidence: `execution/controller/types.go` (`VoidReason` constants); `execution/ledger` writes `GRANT_VOIDED.reason` with all nine
Proposed alternatives: (1) extend the enum to the nine in a contract bump; blast radius: `contracts/`, generated types. (2) map six reasons to one of the three; blast radius: loses the reason on the Record.
Safer reading applied meanwhile (AGENT-20): write the reason as the controller holds it, so the Record keeps the real reason.
Decision required from: founders

## Pre-work for the next agent
- Exact next step: Outcome (OUT-01, OUT-02) verifies the two receipts the outbox holds (`outcome.verify`) against the case by key and predicate; Trust (TRU tasks) replaces `trustStandIn`; the worker in `cmd/core` registers `EscalateCase` and the activities.
- Open questions (owner): the two CONFLICT blocks above (founders); the create descriptor says `CONDITIONAL_WRITE` but mock-case's create takes no expected version (founders, pack owner).
- Files to read first: `docs/architecture.md`, `services/core/execution/e2e/README.md`, `services/core/execution/ledger/README.md`, `services/core/migrations/README.md`.
- Do not touch: `migrations/0001_execution_and_record.sql` after merge (forward-only); `.github/` (needs a founder to run the e2e as its own check if wanted).
