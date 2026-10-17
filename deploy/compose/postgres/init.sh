#!/bin/sh
# Runs once, on first start of an empty data volume. Passwords come from file-mounted secrets (OD-P-22).
set -eu
core_pw="$(cat /run/secrets/noema_core_password)"
temporal_pw="$(cat /run/secrets/temporal_password)"
psql -v ON_ERROR_STOP=1 -v core_pw="$core_pw" -v temporal_pw="$temporal_pw" --username "$POSTGRES_USER" --dbname postgres <<'SQL'
-- The core's role: not a superuser, cannot create roles or databases.
CREATE ROLE noema_core LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'core_pw';
-- Temporal's own role and databases. Noema code never uses them.
CREATE ROLE temporal LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'temporal_pw';
CREATE DATABASE noema OWNER noema_core;
CREATE DATABASE temporal OWNER temporal;
CREATE DATABASE temporal_visibility OWNER temporal;
REVOKE ALL ON DATABASE noema FROM PUBLIC;
REVOKE ALL ON DATABASE temporal FROM PUBLIC;
REVOKE ALL ON DATABASE temporal_visibility FROM PUBLIC;
SQL
