#!/bin/sh
# Runs once, on first start of an empty data volume. Passwords come from file-mounted secrets (OD-P-22).
# W1 D14, D14a: the database is owned by the migration role, never by the core's login; the migrator's credential is read
# here and mounted nowhere else but the one-shot migrate step (W4). The core's login owns nothing and creates nothing.
set -eu
secrets="${NOEMA_SECRETS_DIR:-/run/secrets}"
core_pw="$(cat "$secrets/noema_core_password")"
temporal_pw="$(cat "$secrets/temporal_password")"
migrator_pw="$(cat "$secrets/noema_migrator_password")"
psql -v ON_ERROR_STOP=1 -v core_pw="$core_pw" -v temporal_pw="$temporal_pw" -v migrator_pw="$migrator_pw" \
  --username "$POSTGRES_USER" --dbname postgres <<'SQL'
-- The migration role owns the database and, from migration W4 on, the signed tables. It cannot log in; its one login can.
CREATE ROLE noema_migrator NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION;
CREATE ROLE noema_migration_login LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'migrator_pw';
GRANT noema_migrator TO noema_migration_login;
-- The core's role: not a superuser, cannot create roles or databases, owns nothing. It is never a member of noema_migrator.
CREATE ROLE noema_core LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'core_pw';
-- Temporal's own role and databases. Noema code never uses them.
CREATE ROLE temporal LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'temporal_pw';
-- The six power roles (PERD §38.3). Created here so that no migration needs CREATE ROLE (Architect, 3 Nov 2026). NOLOGIN; the
-- migration login can hand them over (migration 0001: GRANT record_append TO CURRENT_USER) through noema_migrator's admin option.
CREATE ROLE trust_rw NOLOGIN;
CREATE ROLE execution_rw NOLOGIN;
CREATE ROLE outcome_rw NOLOGIN;
CREATE ROLE record_append NOLOGIN;
CREATE ROLE evidence_rw NOLOGIN;
CREATE ROLE orchestrator_rw NOLOGIN;
GRANT record_append TO noema_migrator WITH ADMIN OPTION;
CREATE DATABASE noema OWNER noema_migrator;
CREATE DATABASE temporal OWNER temporal;
CREATE DATABASE temporal_visibility OWNER temporal;
REVOKE ALL ON DATABASE noema FROM PUBLIC;
REVOKE ALL ON DATABASE temporal FROM PUBLIC;
REVOKE ALL ON DATABASE temporal_visibility FROM PUBLIC;
-- The core may connect and nothing more: no CREATE, no TEMPORARY. Rights on tables come from the per-power roles (migrations).
GRANT CONNECT ON DATABASE noema TO noema_core;
\connect noema
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO noema_core;
-- Migration 0001 runs ALTER TABLE ... OWNER TO record_append, which PostgreSQL allows only if the new owner can CREATE in the schema
-- (a superuser is exempt). Held until EXE-08 (W4) transfers both Record tables to noema_migrator and revokes it, before any runtime
-- login can reach the schema; EXE-08's test 7 pins that no runtime role keeps CREATE. Debt: DEBT-05 (status/GOV-28.md).
GRANT CREATE ON SCHEMA public TO record_append;
SQL
