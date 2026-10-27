# Architecture, as built

Date: 2026-10-27 · Status: current · Scope: the parts of `services/core` built for the first end-to-end case (G2 escalate against mock-case).

This file describes what was built. It never defines architecture, requirements or contracts: the SoT, the PERD and `contracts/` do. Where the build differs from them, the difference is in "Deviations" with a pointer.

## Map: module, requirements, source

| Module (`services/core/`) | What it does | Requirements | Source |
|---|---|---|---|
| `migrations/`, `platform/db` | Forward-only SQL migrations, per-power roles, immutability triggers; `db.Migrate`, `db.OpenAsRole` | REQ-DATA-001, REQ-AUD-001 | PERD §38, §32.4 |
| `record/chain` | Append `record-event.v2` events to the per-tenant hash chain inside the caller's transaction; `Verify` | REQ-AUD-001 | PERD §32.1, §32.4; SoT §43.6 |
| `platform/ids` | Prefixed ULIDs from the clock interface | (PERD §32.3) | PERD §32.3 |
| `execution/controller` | Spend-and-dispatch E1 to E11, classification, sweeper; every dependency is a port | REQ-EXE-001..008 | PERD §27 |
| `execution/ledger` | Postgres implementation of the controller's `Ledger` and `GrantSource` | REQ-EXE-001, REQ-EXE-002, REQ-EXE-006, REQ-AUD-001 | PERD §27.1 E8, E11 |
| `execution/bridge` | `CaseDispatcher` (E9) and `CaseRevalidator` (E7) over `adapters.Case`; `GrantExecutor` and `CaseReadBack` for the workflow | REQ-EXE-003, REQ-EXE-004, REQ-EXE-006, REQ-EXE-007, REQ-CAP-002 | PERD §27, §34.4 |
| `adapters`, `adapters/synthetic_tushe` | `Case` interface and its client for mock-case | REQ-ENV-001, REQ-EXE-007 | PERD §34.1, §34.4 |
| `workflows` | Temporal workflow `EscalateCase`; write activities run once, reads retry within a bound | REQ-VER-004, REQ-EXE-006 | PERD §36.1; DEC-2026-10-20 D8 |
| `execution/e2e` | The first end-to-end case, as tests, against the real mock-case | REQ-ENV-001, REQ-EXE-001..007 | status/ENV-03.md |

## How a write flows

1. `workflows.EscalateCase` calls the activity `ExecuteGrant` (one attempt).
2. `bridge.GrantExecutor` runs `controller.Execute`: E1 to E7 read and append `REVALIDATION_*` events; E8 is one SERIALIZABLE transaction in `ledger.Spend` (grant status, kill switch, idempotency record, `grant_spend`, `GRANT_SPENT`, `DISPATCH_ATTEMPTED`); E9 is one call through `bridge.CaseDispatcher`; E10 classifies; E11 is one transaction in `ledger.Complete` (receipt, idempotency record, `RECEIPT`, outbox `outcome.verify`).
3. The workflow reads the case back by the receipt's idempotency key (`ReadBackCase`), asks Trust for a route grant bound to the version token, and runs `ExecuteGrant` again.
4. Nothing says "succeeded". The workflow ends `AWAITING_VERIFICATION`; Outcome's verdict does not exist yet.

## Not built

Trust (decisions, grants, policy), Understanding, Evidence, Outcome (verifier, watches, callbacks), the registry and planner, the other adapters (Tushe-dependent work is paused), the worker process in `cmd/core`, and the other PERD §38.2 tables. In the first end-to-end case a test fixture stands in for Trust's grant issuing and for the plan and intent reads (`execution/e2e/stack_test.go`); no code outside tests does.

## Deviations from the specification

- The create capability's descriptor says `CONDITIONAL_WRITE`. mock-case's create is conditional on absence: it needs `If-None-Match: *`, a unique index on tenant, transaction and epoch enforces it atomically, and a create for a subject that has a case is refused with `412 PRECONDITION_FAILED`. The adapter reports that as the transport class `DUPLICATE_CREATE` (the error code of `errors.v2.json`), so the receipt is `REJECTED_BY_TARGET`, not `REJECTED_STALE_VERSION` (DEC-2026-10-27-record-event-v2, D6). There is no expected version for a create, because there is no case to hold one; the route is conditional on a version token. This holds for mock-case only. Tushe's create semantics are an open item for the founders (`synthetic/mock_case/README.md`, "Create if absent").

Resolved on 27 Oct 2026 (DEC-2026-10-27-record-event-v2): the Record events are `record-event.v2` (PERD §32.1 fields, tenant-scoped genesis hash) and `GRANT_VOIDED.reason` takes any of the nine reasons of PERD §27.1 and REQ-EXE-003. `record-event.v1` and `reconstruction.v1` are unchanged and no longer written.
