#!/bin/sh
# Runs once, on first start of an empty data volume. Passwords come from file-mounted secrets (OD-P-22).
#
# This script creates EVERY role. A migration never creates a role: the migrating role (noema_core) cannot (NOCREATEROLE),
# so a migration that needs CREATE ROLE could not run. Migrations only grant to the roles made here.
set -eu
core_pw="$(cat /run/secrets/noema_core_password)"
temporal_pw="$(cat /run/secrets/temporal_password)"
verify_pw="$(cat /run/secrets/noema_verify_password)"
tenant_pw="$(cat /run/secrets/noema_tenant_login_password)"
psql -v ON_ERROR_STOP=1 -v core_pw="$core_pw" -v temporal_pw="$temporal_pw" -v verify_pw="$verify_pw" --username "$POSTGRES_USER" --dbname postgres <<'SQL'
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

-- The group roles of PERD 38.3 (NOLOGIN). Nothing logs in as a group; a tenant login is a member of one (REQ-DATA-001).
CREATE ROLE trust_rw NOLOGIN;
CREATE ROLE execution_rw NOLOGIN;
CREATE ROLE outcome_rw NOLOGIN;
CREATE ROLE evidence_rw NOLOGIN;
CREATE ROLE orchestrator_rw NOLOGIN;
CREATE ROLE record_append NOLOGIN;
CREATE ROLE record_read NOLOGIN;
CREATE ROLE record_verify NOLOGIN;
-- Migration 0001 hands the Record functions to record_append, which needs membership with the right to grant it.
GRANT record_append TO noema_core WITH ADMIN OPTION;
GRANT CONNECT ON DATABASE noema TO trust_rw, execution_rw, outcome_rw, evidence_rw, orchestrator_rw, record_read, record_verify;

-- The verification job (REC-05) logs in as this; migration 0006 grants record_verify its privileges.
CREATE ROLE noema_verify LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'verify_pw' IN ROLE record_verify;
SQL

# Migration 0001 hands the Record functions to record_append (ALTER FUNCTION ... OWNER TO), and PostgreSQL requires the new owner to
# hold CREATE on the schema. record_append is NOLOGIN and owns only the Record tables and functions.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname noema <<'SQL'
GRANT CREATE ON SCHEMA public TO record_append;
SQL

# One login per tenant and power (DEBT-04 option A, migration 0004): <group>__<tenant>, a member of the group. The mapping rows
# (tenant_login) are added after migration by deploy/compose/postgres/provision-tenants.sh. One shared password file serves all
# of them in the POC; the core holds every login in one process, so the boundary is the database's mapping, not the secret.
for tenant in ${NOEMA_TENANTS:-}; do
  case "$tenant" in
    ten_[0-9A-Za-z][0-9A-Za-z]*) ;;
    *) echo "init: '$tenant' is not a tenant ID" >&2; exit 1 ;;
  esac
  for group in trust_rw execution_rw outcome_rw evidence_rw orchestrator_rw record_read; do
    psql -v ON_ERROR_STOP=1 -v role="${group}__${tenant}" -v grp="$group" -v pw="$tenant_pw" --username "$POSTGRES_USER" --dbname postgres <<'SQL'
CREATE ROLE :"role" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'pw' IN ROLE :"grp";
SQL
  done
done
