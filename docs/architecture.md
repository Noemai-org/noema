# Architecture, as built

Date: 2026-11-03 · Status: current · Scope: the parts of `services/core` built for the first end-to-end case (G2 escalate against mock-case).

This file describes what was built. It never defines architecture, requirements or contracts: the SoT, the PERD and `contracts/` do. Where the build differs from them, the difference is in "Deviations" with a pointer.

## Map: module, requirements, source

| Module (`services/core/`) | What it does | Requirements | Source |
|---|---|---|---|
| `migrations/`, `platform/db` | Forward-only SQL migrations, per-power roles, immutability triggers; `db.Migrate`, `db.OpenAsRole` | REQ-DATA-001, REQ-AUD-001 | PERD §38, §32.4 |
| `record/chain` | Append `record-event.v3` events to the per-tenant hash chain inside the caller's transaction; `Verify` | REQ-AUD-001 | PERD v1.17 §32.1, §32.4; SoT §43.6 |
| `platform/ids` | Prefixed ULIDs from the clock interface | (PERD §32.3) | PERD §32.3 |
| `execution/controller` | Spend-and-dispatch E1 to E11, classification, sweeper; every dependency is a port | REQ-EXE-001..008 | PERD §27 |
| `execution/ledger` | Postgres implementation of the controller's `Ledger` and `GrantSource` | REQ-EXE-001, REQ-EXE-002, REQ-EXE-006, REQ-AUD-001 | PERD §27.1 E8, E11 |
| `execution/bridge` | `CaseDispatcher` (E9) and `CaseRevalidator` (E7) over `adapters.Case`; `GrantExecutor` and `CaseReadBack` for the workflow | REQ-EXE-003, REQ-EXE-004, REQ-EXE-006, REQ-EXE-007, REQ-CAP-002 | PERD §27, §34.4 |
| `adapters`, `adapters/synthetic_tushe` | `Case` interface and its client for mock-case | REQ-ENV-001, REQ-EXE-007 | PERD §34.1, §34.4 |
| `workflows` | Temporal workflow `EscalateCase`; write activities run once, reads retry within a bound | REQ-VER-004, REQ-EXE-006 | PERD §36.1; DEC-2026-10-20 D8 |
| `execution/e2e` | The first end-to-end case, as tests, against the real mock-case | REQ-ENV-001, REQ-EXE-001..007 | status/ENV-03.md |
| `outcome/receiptsig` | Ed25519 verification of an Execution receipt with `core-execution`'s public key (verify-only `PublicKeys`); no private key | REQ-AUD-006, REQ-VER-001 | PERD §12 of DEC-2026-11-01, §29 |
| `outcome/verifier` | Verify by idempotency key after the settle window, from Outcome's own reads (`CaseReader`, no write method); fail closed; verdict signed through a `Signer` port; only a `VALID` receipt goes on to the reads (`ReceiptAuthenticity` port) | REQ-VER-001, REQ-VER-003, REQ-RCV-002 | PERD §29, §30 |
| `outcome/predicate` | Loads a pack's descriptors, variables and outcome definitions and type-checks every success predicate (`internal/predicate`) | REQ-VER-002 | PERD §29.2 |
| `outcome/store` | Postgres `verifier.Store` as `outcome_rw`: loads the receipt, stores verdict, RecoveryRequest, watch, Record events and outbox event in one transaction | REQ-VER-001, REQ-VER-003, REQ-VER-004, REQ-AUD-001 | PERD §29, §32.4 |
| `adapters/outcome_e2e` | US-06, US-16 and EC-E03 end to end against the real mock-case, with the test-only control client | REQ-VER-001, REQ-RCV-002 | DEC-2026-10-26 Amendment 1 |

## How a verdict is made (OUT-01)

0. A verification carries an ID (the delivery's). If a verdict is stored under it, `Verify` returns that verdict and reads and writes nothing; if another delivery wins the insert, the loser returns the winner's verdict.
1. `verifier.Verify` loads the receipt through `store.LoadReceipt` as a pointer (key, principal, `dispatched_at`), compiles the definition against the `case` entity only, and returns `ErrSettleWindowOpen` while `dispatched_at` plus the settle window is ahead of the clock. Nothing is read or written until then.
2. `ReceiptAuthenticity` runs before any read and fails closed: only `VALID` goes on, and `NOT_CHECKED` can never give `ACHIEVED`. `outcome/receiptsig` verifies the receipt's Ed25519 signature with Execution's public key (DEC-2026-11-01 D4, D6, D7, §8); the reader of `signing_key` is W4.
3. Outcome reads the case system itself: by idempotency key, then by transaction within the grant's principal. Each page is read on the retry budget; every attempt is a `VERIFICATION_READ`. An incomplete, malformed, refused, inconsistent or unbound read is `UNKNOWN`, never a negative and never a success.
4. The success predicate is evaluated by `internal/predicate`; ACHIEVED needs a true predicate and the key's case among the transaction's cases.
5. `store.RecordVerdict` is one transaction: the database function `outcome_record_verdict` writes the verdict, its RecoveryRequest (every verdict except ACHIEVED) and its watch (PENDING_EXTERNAL); the Record events and the `recovery.requested` outbox event go in beside them. A failed append stores nothing (EC-E16).

## How a write flows

1. `workflows.EscalateCase` calls the activity `ExecuteGrant` (one attempt).
2. `bridge.GrantExecutor` runs `controller.Execute`: E1 to E7 read and append `REVALIDATION_*` events; E8 is one SERIALIZABLE transaction in `ledger.Spend` (grant status, kill switch, idempotency record, `grant_spend`, `GRANT_SPENT`, `DISPATCH_ATTEMPTED`); E9 is one call through `bridge.CaseDispatcher`; E10 classifies; E11 is one transaction in `ledger.Complete` (receipt, idempotency record, `RECEIPT`, outbox `outcome.verify`).
3. The workflow reads the case back by the receipt's idempotency key (`ReadBackCase`), asks Trust for a route grant bound to the version token, and runs `ExecuteGrant` again.
4. Nothing says "succeeded". The workflow ends `AWAITING_VERIFICATION`. Outcome's verdict is a separate call (below); nothing triggers it yet.

## Not built

Trust (decisions, grants, policy), Understanding, Evidence, Outcome's watch workflow (OUT-03), callbacks (REQ-VER-005) and the inbound event inbox (OUT-04), the trigger that calls `Verify` from the `outcome.verify` outbox event, the registry and planner, the other adapters (Tushe-dependent work is paused), the worker process in `cmd/core`, and the other PERD §38.2 tables. In the first end-to-end case a test fixture stands in for Trust's grant issuing and for the plan and intent reads (`execution/e2e/stack_test.go`); no code outside tests does.

## Deviations from the specification

- The create capability's descriptor says `CONDITIONAL_WRITE`. mock-case's create is conditional on absence: it needs `If-None-Match: *`, a unique index on tenant, transaction and epoch enforces it atomically, and a create for a subject that has a case is refused with `412 PRECONDITION_FAILED`. The adapter reports that as the transport class `DUPLICATE_CREATE` (the error code of `errors.v2.json`), so the receipt is `REJECTED_BY_TARGET`, not `REJECTED_STALE_VERSION` (DEC-2026-10-27-record-event-v2, D6). There is no expected version for a create, because there is no case to hold one; the route is conditional on a version token. This holds for mock-case only. Tushe's create semantics are an open item for the founders (`synthetic/mock_case/README.md`, "Create if absent").

Resolved on 27 Oct 2026 (DEC-2026-10-27-record-event-v2 and DEC-2026-10-27-actor-and-payload-hash): the Record events are `record-event.v3` (PERD v1.17 §32.1: tenant-scoped genesis hash, an emitter in `written_by`, a required principal in `actor`, `payload_hash` = SHA-256 of the RFC 8785 form with integers only) and `GRANT_VOIDED.reason` takes any of the nine reasons of PERD §27.1 and REQ-EXE-003. `record-event.v1` and `record-event.v2` (and their reconstruction schemas) are unchanged and no longer written. The SoT §43.6 example `"written_by": "TRUST"` is illustrative; the PERD narrows the field to the 17 service identities, and the next SoT version should change the example.
