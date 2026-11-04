# migrations

Forward-only SQL migrations, never edited after merge (AGENTS.md section 7, PERD §38; task PLT-06). The files are embedded by `embed.go` and applied by `platform/db` (`db.Migrate`): in file-name order, each in its own transaction, recorded in `schema_migration` with a checksum. A file that changed after it was applied, or was removed, makes `Migrate` fail.

**Implements.** REQ-DATA-001, REQ-AUD-001, REQ-EXE-001, REQ-EXE-002, REQ-VER-001, REQ-VER-003, REQ-VER-004.

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
| `record_event`, `record_chain_head` | `record_event.schema_version` is "3.0" (`record-event.v3`; `written_by` is an emitter and `actor` a principal, PERD v1.17 §32.1); owned by `record_append`; no power holds any privilege; reached only through `record_lock_head` and `record_insert` |

Per-power roles (NOLOGIN): `trust_rw`, `execution_rw`, `outcome_rw`, `record_append`, `evidence_rw`, `orchestrator_rw`. The deployment maps its login roles to them. The migrating role must be a superuser or able to hand objects to `record_append`.

`0007_outcome_verdict.sql` (OUT-01). Numbering: `0002` is reserved for noema#94, `0003` is the `record_read` migration and `0004` to `0006` are DEBT-04's (noema#145); OUT-01 took `0007` so that no two migrations share a number. The migrator applies by name and accepts a gap (tested on a fresh database and on an upgrade where `0002` arrives after `0007`).

| Object | Rule |
|---|---|
| `outcome_verdict` | insert-only; one verdict per `(tenant_id, verification_id)` (a redelivery of `outcome.verify` finds it); ACHIEVED needs a true predicate and at least one evidence read; NOT_ACHIEVED needs a false one (outcome-verdict.v1) |
| `outcome_watch` | one per PENDING_EXTERNAL verdict, with a deadline at or after its first check; never deleted; only `status` and `next_check_at` may change (OUT-03) |
| `recovery_request` | now from a void grant **or** a non-ACHIEVED verdict, exactly one (`grant_id` XOR `verdict_id`); privileges unchanged: Execution cannot forge a verdict-sourced request (a guard trigger and the unique `verdict_id` refuse it) |
| `outcome_record_verdict(...)` | the only write path for the three tables above; it refuses a call from a login that also holds another per-power role (it checks `session_user`, so `SET ROLE outcome_rw` from a superuser or a multi-power login is refused); it refuses a verdict whose correlation is not the receipt's grant's or whose signed document disagrees with its columns on any field both hold (ids, verdict, evidence reads, predicate result, instant, signature); `SECURITY DEFINER`; `EXECUTE` for `outcome_rw` alone; `outcome_rw` has no INSERT, UPDATE or DELETE on them |
| deferred constraint trigger | at commit, for every inserter: a verdict has a recovery request exactly when it is not ACHIEVED, and a watch exactly when it is PENDING_EXTERNAL |

**Deployment rule.** Outcome's login holds the `outcome_rw` power and no other per-power role, and connects as that login (then `SET ROLE outcome_rw`). `outcome_record_verdict` authenticates the calling login this way; a superuser or a login that also holds `trust_rw`, `execution_rw`, `evidence_rw` or `orchestrator_rw` is refused with `42501`.

**Known limit (accepted, DEBT-03; until REC-08, REQ-AUD-006).** Anyone holding Outcome's own login credential can call `outcome_record_verdict`. The function bounds what is written (a well-formed verdict with its request, watch and events), not who wrote it. Signed Record events close this: the database will refuse an event whose signature does not verify against Outcome's trusted public key.

## Not built

- The other PERD §38.2 tables (`principal_ref`, `session`, `investigation`, `intent_object`, `decision_record`, `plan`, `context_snapshot`, ...). Foreign keys from `decision_grant` to `decision_record` and `plan` arrive with the migration that creates those tables. Until then Execution reads the plan and the intent through the `Binder` port.
- `tenant_id` is on every row and every query is tenant-scoped; row-level security is not used. A repository-layer test that every query is tenant-scoped is not written yet (REQ-SEC-004). Debt entry to be filed in `docs/poc/execution/debt.md` (docs repository).

## Tests

`go test ./platform/db/...` from `services/core/`. They start a throwaway PostgreSQL server (`internal/testpg`), or use `NOEMA_TEST_DATABASE_URL` if it is set.
