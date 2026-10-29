# migrations

Forward-only SQL migrations, never edited after merge (AGENTS.md section 7, PERD §38; task PLT-06). The files are embedded by `embed.go` and applied by `platform/db` (`db.Migrate`): in file-name order, each in its own transaction, recorded in `schema_migration` with a checksum. A file that changed after it was applied, or was removed, makes `Migrate` fail.

**Implements.** REQ-DATA-001, REQ-AUD-001, REQ-EXE-001, REQ-EXE-002.

## What is built

`0001_execution_and_record.sql`, the slice of PERD §38.2 that the first end-to-end case needs:

| Table | Mutability |
|---|---|
| `tenant` | mutable (the kill switch); written by the administrator role only |
| `decision_grant` | the contract object never changes; only `status` moves, to a final state (trigger); `execution_rw` holds `UPDATE (status)` and nothing else. From `0002`: `SPENT → VOID` is also allowed, only for a confirmed `REJECTED_STALE_VERSION` refusal (below) |
| `grant_spend` | insert-only; unique on `grant_id` and on `nonce` (single use, INV-30) |
| `idempotency_record` | mutable `attempt`, `status`, `external_ref`; key, action and parameters never change; the attempt only moves forward |
| `execution_receipt` | insert-only; unique on (`grant_id`, `attempt`) |
| `recovery_request` | `execution_rw` inserts, `trust_rw` updates `status`; unique per grant |
| `outbox_event` | `orchestrator_rw` marks it dispatched |
| `record_event`, `record_chain_head` | `record_event.schema_version` is "3.0" (`record-event.v3`; `written_by` is an emitter and `actor` a principal, PERD v1.17 §32.1); owned by `record_append`; no power holds any privilege; reached only through `record_lock_head` and `record_insert` |

`0002_grant_spent_void.sql` (PERD v1.18 §38.2, REQ-EXE-004, `DEC-2026-10-28-spent-grant-void`): replaces the function behind `decision_grant_guard` (the triggers are untouched; `0001` is not edited). `ISSUED → SPENT, VOID or EXPIRED` is as before. `SPENT → VOID` is allowed only for a **confirmed refusal**, which the trigger checks by reading `execution_receipt` and `grant_spend` in the same transaction. It reads the signed receipt document (`execution_receipt.json`, `execution-receipt.v1`) and cross-checks it against the row's columns and the spend: exactly one receipt for the grant; its tenant, grant, attempt and idempotency key are the spend's, in the row and in the document; `execution_state` is `REJECTED_STALE_VERSION` in both; the typed member `transport_result` is `VERSION_CONFLICT_409_412` (the class the case adapter and the controller share, which the adapter gives only to a target's version refusal: mock-case `409 VERSION_MISMATCH` on a route; its `412 PRECONDITION_FAILED` is a refused create and gives `DUPLICATE_CREATE`); `external_ack` is null in the document and null or empty in the row; the document's `sig` member equals the row's signature columns, the key is not empty and the value is 64 lower-case hex characters (the shape of an HMAC-SHA-256; the database cannot verify the HMAC, it holds no key). `raw_status` is a diagnostic: it decides nothing, but its class must agree with `transport_result`, or the receipt is ambiguous and refused. **Limitations:** the receipt contract fixes the shape of `transport_result` but no value set, so the string is Noema's own constant; the Controller verifies the receipt's HMAC (`KeyRing.VerifyWith`) before the Ledger writes it, and a failed verification writes no receipt; the SoT names Outcome, not built, as a further checker; and only `execution_rw` can insert a receipt; and the "nothing applied" guarantee behind the confirmation comes from `mock-case`'s OpenAPI contract, not from a real target's contract, and must be re-verified against any real target before production use. Every other change out of `SPENT`, `VOID` or `EXPIRED`, a `DELETE`, and any change of another column stay refused. Only `execution_rw` holds the privilege to change a status.

Per-power roles (NOLOGIN): `trust_rw`, `execution_rw`, `outcome_rw`, `record_append`, `evidence_rw`, `orchestrator_rw`. The deployment maps its login roles to them. The migrating role must be a superuser or able to hand objects to `record_append`.

## Not built

- The other PERD §38.2 tables (`principal_ref`, `session`, `investigation`, `intent_object`, `decision_record`, `plan`, `context_snapshot`, `outcome_verdict`, ...). Foreign keys from `decision_grant` to `decision_record` and `plan` arrive with the migration that creates those tables. Until then Execution reads the plan and the intent through the `Binder` port.
- `tenant_id` is on every row and every query is tenant-scoped; row-level security is not used. A repository-layer test that every query is tenant-scoped is not written yet (REQ-SEC-004). Debt entry to be filed in `docs/poc/execution/debt.md` (docs repository).

## Tests

`go test ./platform/db/...` from `services/core/` (`grant_void_test.go` holds the guard regression tests: the permitted transition and each refused one). They start a throwaway PostgreSQL server (`internal/testpg`), or use `NOEMA_TEST_DATABASE_URL` if it is set.
