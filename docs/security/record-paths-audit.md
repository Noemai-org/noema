# Record paths audit (ruling A20)

**6 Nov 2026 (Core Engineer 1), for the Security Reviewer.** As-built, not authority: the SoT, the PERD and the rulings decide. SYNTHETIC data only. Ruling A20: audit reads the Record through read functions only, and neither audit nor operators have a path to a Record writer. This is the list of what I checked, so that SR can test what is **not** on it. The checks are repeatable: `services/core/platform/db/record_paths_test.go`, which reads the privileges the database holds. They were not written failing-first (the privileges already hold on `main`); a scratch grant of `INSERT` on `record_event` to `record_verify` and of `EXECUTE` on `record_insert_t` to `record_read` turned four of them red, then I reverted it.

## Roles and what each holds on the Record (migrations 0001 to 0014 as of this branch)

| Role | Record tables (`record_event`, `record_chain_head`) | Login map (`tenant_login`) | Record functions it may execute |
|---|---|---|---|
| `record_read` (audit) | nothing | nothing | `record_read_chain_t()` only (tenant from `session_user`, never an argument) |
| `record_verify` (chain verifier, REC-05) | `SELECT` on both, across tenants, by design; no write | nothing | none |
| `trust_rw`, `execution_rw`, `outcome_rw`, `evidence_rw`, `orchestrator_rw`, `record_emitter` | nothing | nothing | `record_lock_head_t()`, `record_insert_t(...)`; `outcome_rw` also `outcome_record_verdict(...)` |
| `record_append` (owner of the functions; no login is a member) | `SELECT`, `INSERT` (events); `SELECT`, `INSERT`, `UPDATE` (heads) | full | all of them, incl. `provision_tenant_login`, `record_login_tenant` |
| operators (`ops`, A1) | **no database role exists**; an operator reaches the database only through the core's own logins | | |

## Paths checked

1. **Table privileges** of every runtime role on `record_event`, `record_chain_head`, `tenant_login` for `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE` (`has_table_privilege`).
2. **Function privileges** (`has_function_privilege`) of every runtime role on `record_insert_t`, `record_lock_head_t`, `record_login_tenant`, `provision_tenant_login`, `provision_emitter_login` (the last exists only once noema#181 merges; the check is then live).
3. **Who may call each Record function, exactly** (a missing grant fails as surely as an extra one): the five powers, `record_emitter` and `record_append` for `record_insert_t` and `record_lock_head_t`; `record_append` and `record_read` for `record_read_chain_t`; `record_append` alone for `record_login_tenant` and `provision_tenant_login`. A function that does not exist fails the check instead of reading as 'nobody can call it'. `provision_emitter_login` arrives with noema#181: its test is skipped, visibly, until then, so it is **not covered**.
4. **Operator or audit roles in the catalog**: none matching `ops`, `operator` or `audit`.
5. **Search-path hijack of the writers** (found and closed, noema#234, migration 0014): a login with `TEMPORARY` could shadow `tenant_login` and be mapped to another tenant. All five functions owned by `record_append` now pin `pg_catalog, pg_temp`.
6. **Immutability** of the Record (`record_event_immutable`, `record_event_no_truncate`): already tested in `platform/db/schema_test.go` and `record/chain`; not repeated here.

## Findings for SR and the Engineering Lead (not decided here)

- **F1. `record_verify` reads the Record tables directly and across tenants.** That is the verifier's job (REC-05), not audit, but A20 says "audit reads via read functions only". If the Founders mean the verifier too, it needs a cross-tenant read function; today it is a table grant. The test pins the grant exactly so any widening fails.
- **F2. No row-level security on the Record tables yet** (the rest of A6, not built): a role that holds `SELECT` sees every tenant's rows. Only `record_verify` holds it, by design. Planned: RLS + `FORCE` on these tables, stacked on noema#168's `decision_login_tenant()`; the verifier needs an explicit all-tenants policy.
- **F3. `PUBLIC` can execute two trigger functions** (`outcome_verdict_complete`, `outcome_watch_guard`). They return `trigger` and cannot be called directly; recorded for completeness.
- **F4. Not checked:** the core process itself holds every login (DEBT-13); `noema_migrator` and the superuser are outside the runtime roles by design (D14); network-level access to the database; the Temporal database.

**Status of the control:** the grants above are evidence of the *current design*, not proof of tenant isolation. F1 and F2 stay open for the Security Reviewer and the decision owner; this note does not call the A20 control proven.

Nothing here changes a ruling, threshold or contract.
