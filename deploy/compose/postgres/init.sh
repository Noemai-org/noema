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
SQL
