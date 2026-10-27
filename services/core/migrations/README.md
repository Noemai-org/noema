# migrations

Forward-only SQL migrations, never edited after merge (AGENTS.md section 7, PERD §38; task PLT-06). The files are embedded by `embed.go` and applied by `platform/db` (`db.Migrate`): in file-name order, each in its own transaction, recorded in `schema_migration` with a checksum. A file that changed after it was applied, or was removed, makes `Migrate` fail.

**Implements.** REQ-DATA-001, REQ-AUD-001, REQ-EXE-001, REQ-EXE-002.

## What is built

`0001_execution_and_record.sql`, the slice of PERD §38.2 that the first end-to-end case needs:

| Table | Mutability |
|---|---|
| `tenant` | mutable (the kill switch); written by the administrator role only |
| `decision_grant` | the contract object never changes; only `status` moves, once, to a final state (trigger); `execution_rw` holds `UPDATE (status)` and nothing else |
| `grant_spend` | insert-only; unique on `grant_id` and on `nonce` (single use, INV-30) |
| `idempotency_record` | mutable `attempt`, `status`, `external_ref`; key, action and parameters never change; the attempt only moves forward |
| `execution_receipt` | insert-only; unique on (`grant_id`, `attempt`) |
| `recovery_request` | `execution_rw` inserts, `trust_rw` updates `status`; unique per grant |
| `outbox_event` | `orchestrator_rw` marks it dispatched |
| `record_event`, `record_chain_head` | `record_event.schema_version` is "2.0" (`record-event.v2`); owned by `record_append`; no power holds any privilege; reached only through `record_lock_head` and `record_insert` |

Per-power roles (NOLOGIN): `trust_rw`, `execution_rw`, `outcome_rw`, `record_append`, `evidence_rw`, `orchestrator_rw`. The deployment maps its login roles to them. The migrating role must be a superuser or able to hand objects to `record_append`.

## Not built

- The other PERD §38.2 tables (`principal_ref`, `session`, `investigation`, `intent_object`, `decision_record`, `plan`, `context_snapshot`, `outcome_verdict`, ...). Foreign keys from `decision_grant` to `decision_record` and `plan` arrive with the migration that creates those tables. Until then Execution reads the plan and the intent through the `Binder` port.
- `tenant_id` is on every row and every query is tenant-scoped; row-level security is not used. A repository-layer test that every query is tenant-scoped is not written yet (REQ-SEC-004). Debt entry to be filed in `docs/poc/execution/debt.md` (docs repository).

## Tests

`go test ./platform/db/...` from `services/core/`. They start a throwaway PostgreSQL server (`internal/testpg`), or use `NOEMA_TEST_DATABASE_URL` if it is set.
