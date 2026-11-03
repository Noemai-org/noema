# Status · GOV-28 (W1) · init.sh follow-up

State: In review (this change). Owner: Engineering Lead role agent. Reviewer: the Security Reviewer validates.

## Session 2026-11-03 · init.sh: migrator credential and database owner (W1 D14a, §6.1 second deviation)
## Session summary
- What changed: `deploy/compose/postgres/init.sh` creates `noema_migrator` (NOLOGIN) and `noema_migration_login`, makes `noema_migrator` the owner of the `noema` database, gives `noema_core` only CONNECT and USAGE on `public` (no CREATE, no TEMPORARY), and reads the new secret `noema_migrator_password`, mounted into `postgres` only. `assert_roles.sql` pins it; `netcheck/matrix.py` runs it against the stack and checks which services hold the secret; `postgres/test_init.sh` runs init.sh on a throwaway cluster and connects as `noema_core`.
- Why: the Architect's order of 3 Nov 2026 (item 6), W1 D14a and the second deviation of §6.1: the core's login owned the database.
- Risks, and how they are contained: (1) Migration `0001` runs `CREATE ROLE` and `GRANT record_append TO CURRENT_USER`, which `noema_migration_login` (no CREATEROLE) cannot do. Nothing runs migrations in Compose today, so nothing breaks; the choice between that login and the superuser for the migrate step is W4's (EXE-08, W1 D14), and EXE-08 must either pre-create the six power roles in the bootstrap or grant CREATEROLE narrowly. (2) W4's migration must create `noema_migrator` with `IF NOT EXISTS`, because the bootstrap now creates it. (3) `noema_core` has no membership of the power roles yet; W4 grants them `NOINHERIT`.
- Tests added: `assert_roles.sql` (owner, no CREATE or TEMPORARY, no owned objects, no recursive membership of `noema_migrator`, role attributes), `test_init.sh` (connects as `noema_core`; CREATE TABLE, CREATE SCHEMA, temp table and SET ROLE noema_migrator are refused), `check_migrator_boundary` in `matrix.py`. Seen failing first: owner was `noema_core` (commit "failing test first").
- Debt entries: closes the database-owner part of DEBT-04.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EXE-08 (W4) builds `cmd/migrate`, the one-shot `migrate` service and the `core` dependency on it; it holds the second copy of `noema_migrator_password`.
- Do not touch: `noema#94`, `noema#90`.

## Session 2026-11-03 (later) · Architect's decision 6: init.sh creates all roles; no migration may need CREATE ROLE
- Done: `init.sh` creates the six power roles (`trust_rw`, `execution_rw`, `outcome_rw`, `record_append`, `evidence_rw`, `orchestrator_rw`, NOLOGIN) and gives `noema_migrator` `record_append` with the admin option. `assert_roles.sql` pins that they exist, cannot log in and are not members of anything that reaches `noema_migrator`. `test_init.sh` now applies migration `0001` as `noema_migration_login` (a plain role) and passes: no CREATE ROLE is needed.
- Finding to resolve with the Security Reviewer: with a non-superuser migration login, `0001` line 207 (`ALTER TABLE record_event OWNER TO record_append`) fails with "permission denied for schema public", because PostgreSQL requires the new owner to have CREATE on the schema (a superuser is exempt). Options: (a) the migrate step runs as the bootstrap superuser (a superuser credential in the migrate step; larger blast radius); (b) the bootstrap grants `record_append` CREATE on `public` until W4's migration transfers the tables to `noema_migrator` and revokes it (done here; the window ends before any runtime login can use the schema, and EXE-08's test 7 must pin it). **Recommendation: (b).** `0001` is immutable, so it cannot be edited out.
- Debt entry proposed: DEBT-05, `record_append` holds CREATE on `public` until EXE-08. Owner: Engineering Lead (EXE-08 is C1). Closes with EXE-08 test 7. (The Steward adds it to `debt.md`; I do not edit the register from here.)
