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
| `record_event`, `record_chain_head` | `record_event.schema_version` is "3.0" (`record-event.v3`; `written_by` is an emitter and `actor` a principal, PERD v1.17 §32.1); owned by `record_append`; no power holds any privilege; reached only through `record_lock_head_t` and `record_insert_t` (writes) and `record_read_chain_t` (reads), which take the tenant from the login (0004) |

`0003_record_read.sql` (REC-03, approved escalation E1 of 1 Nov 2026): the role `record_read` (NOLOGIN) and the function `record_read_chain(tenant)`, owned by `record_append`. `EXECUTE` is revoked from PUBLIC and granted only to `record_read`, which holds nothing else on the Record. It is numbered `0003` because noema#94 holds `0002`. `Migrate` has no gap check: it applies every unapplied file in name order, so a database migrated to `0003` takes a later `0002` after it. `platform/db/record_read_test.go` proves that both orders give the same privileges.

`0004_tenant_bound_record.sql` and `0005_drop_untenanted_record_functions.sql` (DEBT-04 option A): the `tenant_login` table maps each LOGIN role to one tenant and one power; `record_lock_head_t()`, `record_insert_t(...)` and `record_read_chain_t()` find the tenant with `session_user`, which a caller cannot change (not by `SET ROLE`, and only a superuser by `SET SESSION AUTHORIZATION`); `record_insert_t` also refuses an event whose body names another tenant. `0005` drops the three functions that took the tenant as an argument. A deployment creates one LOGIN role per tenant and power (for example `execution_rw__ten_aa`, a member of `execution_rw`) and one `tenant_login` row for it.

`0006_record_verify_and_provisioning.sql` (DEBT-04 option A, slice 3): grants the group role `record_verify` SELECT on the Record, the chain heads and the tenant list, and UPDATE on `tenant.kill_switch` only (the verification job, REC-05); and adds `provision_tenant_login(login, power, tenant)`, callable only by the migrating role, which maps an existing login to a tenant after checking that it can log in and belongs to its power's group.

**No migration after 0001 creates a role** (`TestNoMigrationAfter0001CreatesARole`): the migrating role (`noema_migration_login` in Compose) is NOCREATEROLE. `deploy/compose/postgres/init.sh` creates every role, and a migration that needs one checks that it exists and stops with a message that says where to create it. 0001 predates the rule; its guarded creation does nothing when `init.sh` has run first. A database that was initialised before this rule needs the roles created by hand once (initdb scripts run only on an empty volume).

Per-power roles (NOLOGIN): `trust_rw`, `execution_rw`, `outcome_rw`, `record_append`, `evidence_rw`, `orchestrator_rw` (0001) and `record_read` (0003). The deployment maps its login roles to them. The migrating role must be a superuser or able to hand objects to `record_append`.

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

`0008_evidence_tables.sql` (EVD-01 slice 2a): `evidence_payload` and `evidence_item` (PERD 38.2, SoT 43.5), insert-only by trigger, an item pointing at a payload of its own tenant by a composite key; `evidence_rw` writes (a trigger binds each write to the tenant and power of the login, `session_user`), `trust_rw` and `outcome_rw` read; row-level security limits every read and write to the login's own tenant. Numbered 0008 because 0007 is the Outcome verdict migration (OUT-01); migrations apply by name, so the gap is harmless.

`0010_record_tables_to_migrator.sql` (EXE-08 slice A, closes DEBT-05): `record_event` and `record_chain_head` are owned by `noema_migrator` instead of `record_append`; `record_append` keeps INSERT and SELECT on the event table and SELECT, INSERT and UPDATE on the head table as grants its definer functions need; `record_append` loses `CREATE` on `public`. The role `noema_migrator` is created by `init.sh` (checked, never created, by the migration). A later migration that must hand a function to `record_append` grants `CREATE ON SCHEMA public` to it at its start and revokes it at its end, in the same transaction. Numbers 0008 and 0009 are taken by open pull requests (noema#167, noema#168).

`0013_receipt_signature.sql` (EXE-08 slice B, DEBT-01): `signing_key` (public keys only, K1, rotation and revocation), `key_register`, `key_retire`, `key_revoke`, `noema_sig.verify` (pgsodium), `execution_receipt.signed_jcs`, the always-enabled receipt trigger that raises `NS001`, and `receipt_insert`; `execution_rw` no longer inserts into `execution_receipt`. Prerequisite: `init.sh` creates pgsodium once and disables its event triggers (D10a); `db.Migrate` refuses while one is enabled. Number: the next free one at the time of writing; whichever of the open migrations merges later renumbers (D3). `noema#94` follows this one and is renumbered after it.

`0018_record_event_signature.sql` (REC-08, DEBT-03; number 0018 by Founder decision D12): `record_event` gains `sig_key_id`, `sig_value` and `signed_jcs`; a `BEFORE INSERT` trigger (`ENABLE ALWAYS`, owned by `noema_migrator`) checks that the signed bytes are the stored body and name the row's own tenant, `seq`, `prev_hash`, `event_id` and `written_by`, and verifies the Ed25519 signature over `noema/record-event/v1\n` + the signed bytes under an ACTIVE key of the `written_by` identity (`noema_sig.verify`, migration 0012); it raises `NS001` otherwise and fails closed when verification is unavailable. `record_insert_t` takes the signature, key ID and signed bytes as three more arguments; the unsigned form is dropped. An emitter with no registered key (the model service and the mocks, the OD-P-73 default) cannot append. The signature is in columns, not in the event document: `record-event.v4` (D1) is a separate change. `record_verify` can read `signing_key`, so the chain verifier checks every signature in Go.
