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
| `record_event`, `record_chain_head` | `record_event.schema_version` is "3.0" (`record-event.v3`; `written_by` is an emitter and `actor` a principal, PERD v1.17 §32.1); owned by `record_append`; no power holds any privilege; reached only through `record_lock_head_t` and `record_insert_t` (writes) and `record_read_chain_t` (reads), which take the tenant from the login (0004) |

`0003_record_read.sql` (REC-03, approved escalation E1 of 1 Nov 2026): the role `record_read` (NOLOGIN) and the function `record_read_chain(tenant)`, owned by `record_append`. `EXECUTE` is revoked from PUBLIC and granted only to `record_read`, which holds nothing else on the Record. It is numbered `0003` because noema#94 holds `0002`. `Migrate` has no gap check: it applies every unapplied file in name order, so a database migrated to `0003` takes a later `0002` after it. `platform/db/record_read_test.go` proves that both orders give the same privileges.

`0004_tenant_bound_record.sql` and `0005_drop_untenanted_record_functions.sql` (DEBT-04 option A): the `tenant_login` table maps each LOGIN role to one tenant and one power; `record_lock_head_t()`, `record_insert_t(...)` and `record_read_chain_t()` find the tenant with `session_user`, which a caller cannot change (not by `SET ROLE`, and only a superuser by `SET SESSION AUTHORIZATION`); `record_insert_t` also refuses an event whose body names another tenant. `0005` drops the three functions that took the tenant as an argument. A deployment creates one LOGIN role per tenant and power (for example `execution_rw__ten_aa`, a member of `execution_rw`) and one `tenant_login` row for it.

`0006_record_verify_and_provisioning.sql` (DEBT-04 option A, slice 3): grants the group role `record_verify` SELECT on the Record, the chain heads and the tenant list, and UPDATE on `tenant.kill_switch` only (the verification job, REC-05); and adds `provision_tenant_login(login, power, tenant)`, callable only by the migrating role, which maps an existing login to a tenant after checking that it can log in and belongs to its power's group.

**No migration after 0001 creates a role** (`TestNoMigrationAfter0001CreatesARole`): the migrating role (`noema_migration_login` in Compose) is NOCREATEROLE. `deploy/compose/postgres/init.sh` creates every role, and a migration that needs one checks that it exists and stops with a message that says where to create it. 0001 predates the rule; its guarded creation does nothing when `init.sh` has run first. A database that was initialised before this rule needs the roles created by hand once (initdb scripts run only on an empty volume).

Per-power roles (NOLOGIN): `trust_rw`, `execution_rw`, `outcome_rw`, `record_append`, `evidence_rw`, `orchestrator_rw` (0001) and `record_read` (0003). The deployment maps its login roles to them. The migrating role must be a superuser or able to hand objects to `record_append`.

## Not built

- The other PERD §38.2 tables (`principal_ref`, `session`, `investigation`, `intent_object`, `decision_record`, `plan`, `context_snapshot`, `outcome_verdict`, ...). Foreign keys from `decision_grant` to `decision_record` and `plan` arrive with the migration that creates those tables. Until then Execution reads the plan and the intent through the `Binder` port.
- `tenant_id` is on every row and every query is tenant-scoped; row-level security is not used. A repository-layer test that every query is tenant-scoped is not written yet (REQ-SEC-004). Debt entry to be filed in `docs/poc/execution/debt.md` (docs repository).

## Tests

`go test ./platform/db/...` from `services/core/`. They start a throwaway PostgreSQL server (`internal/testpg`), or use `NOEMA_TEST_DATABASE_URL` if it is set.

`0008_evidence_tables.sql` (EVD-01 slice 2a): `evidence_payload` and `evidence_item` (PERD 38.2, SoT 43.5), insert-only by trigger, an item pointing at a payload of its own tenant by a composite key; `evidence_rw` writes, `trust_rw` and `outcome_rw` read. Numbered 0008 because 0007 is the Outcome verdict migration (OUT-01); migrations apply by name, so the gap is harmless.
