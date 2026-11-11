#!/bin/sh
# Runs once, on first start of an empty data volume. Passwords come from file-mounted secrets (OD-P-22).
# W1 D14, D14a: the database is owned by the migration role, never by the core's login; the migrator's credential is read
# here and mounted nowhere else but the one-shot migrate step (W4). The core's login owns nothing and creates nothing.
set -eu
secrets="${NOEMA_SECRETS_DIR:-/run/secrets}"
core_pw="$(cat "$secrets/noema_core_password")"
temporal_pw="$(cat "$secrets/temporal_password")"
migrator_pw="$(cat "$secrets/noema_migrator_password")"
verify_pw="$(cat "$secrets/noema_verify_password")"
tenant_pw="$(cat "$secrets/noema_tenant_login_password")"

# A4 (Architect ruling, 6 Nov 2026): each emitter login has its own file-mounted secret, $secrets/emitter_logins/<tenant>__<emitter>. Checked
# before anything is created: every file must exist, be non-empty and differ from every other secret, including the other emitter logins'.
# The shared tenant-login password never opens an emitter login. A refusal names the file, never a value. DEBT-13 extends this to the
# tenant and power logins before G3 evidence.
seen="$(printf '%s\n' "$core_pw" "$temporal_pw" "$migrator_pw" "$verify_pw" "$tenant_pw" "$(cat "$secrets/postgres_superuser_password" 2>/dev/null || true)")"
for tenant in ${NOEMA_TENANTS:-}; do
  case "$tenant" in
    ten_[0-9A-Za-z][0-9A-Za-z]*) ;;
    *) echo "init: '$tenant' is not a tenant ID" >&2; exit 1 ;;
  esac
  for emitter in ${NOEMA_EMITTERS:-core-edge}; do
    case "$emitter" in
      core-trust|core-execution|core-outcome|core-evidence|core-workflows) echo "init: '$emitter' is a power's identity" >&2; exit 1 ;;
      '') echo "init: an emitter name is empty" >&2; exit 1 ;;
      *[!a-z0-9-]*|[!a-z]*) echo "init: '$emitter' is not an emitter name (^[a-z][a-z0-9-]*\$)" >&2; exit 1 ;;
      *) ;;
    esac
    f="$secrets/emitter_logins/${tenant}__${emitter}"
    [ -f "$f" ] && [ -s "$f" ] || { echo "init: the emitter login ${tenant}__${emitter} has no secret file of its own ($f)" >&2; exit 1; }
    pw="$(cat "$f")"
    if printf '%s\n' "$seen" | grep -Fxq -- "$pw"; then echo "init: the secret of the emitter login ${tenant}__${emitter} equals another secret ($f)" >&2; exit 1; fi
    seen="$(printf '%s\n%s\n' "$seen" "$pw")"
  done
done
psql -v ON_ERROR_STOP=1 -v core_pw="$core_pw" -v temporal_pw="$temporal_pw" -v migrator_pw="$migrator_pw" -v verify_pw="$verify_pw" \
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
-- The read and verification group roles (REC-03, REC-05; DEBT-04 option A). Migrations 0003 and 0006 grant to them and never create them.
CREATE ROLE record_read NOLOGIN;
CREATE ROLE record_verify NOLOGIN;
-- The Record-only role of a login bound to an emitter that is not a power (DEBT-07): migration 0004 grants it the append functions and nothing else.
CREATE ROLE record_emitter NOLOGIN;
-- The verification job logs in as this; migration 0006 grants record_verify its privileges.
CREATE ROLE noema_verify LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'verify_pw' IN ROLE record_verify;
CREATE DATABASE noema OWNER noema_migrator;
CREATE DATABASE temporal OWNER temporal;
CREATE DATABASE temporal_visibility OWNER temporal;
REVOKE ALL ON DATABASE noema FROM PUBLIC;
REVOKE ALL ON DATABASE temporal FROM PUBLIC;
REVOKE ALL ON DATABASE temporal_visibility FROM PUBLIC;
-- The core may connect and nothing more: no CREATE, no TEMPORARY. Rights on tables come from the per-power roles (migrations).
GRANT CONNECT ON DATABASE noema TO noema_core, trust_rw, execution_rw, outcome_rw, evidence_rw, orchestrator_rw, record_read, record_verify, record_emitter;
\connect noema
-- DEC-2026-11-01 D10, D10a (EXE-08 slice B, ruling A2): pgsodium verifies Ed25519 signatures in the database; the private keys never are in it. The extension
-- is created without shared_preload_libraries (no server key management). Its event trigger reads a setting that exists only when the library is preloaded,
-- so it is disabled here, once, as a superuser, before any Noema migration, and the setting is off. Nothing in its schema is executable by PUBLIC; only
-- noema_migrator may call the one verification function noema_sig.verify uses. db.Migrate and the core refuse to run if the event trigger is enabled again.
CREATE EXTENSION pgsodium;
DO $$ DECLARE r record; BEGIN
  FOR r IN SELECT e.evtname FROM pg_event_trigger e JOIN pg_depend d ON d.objid = e.oid AND d.deptype = 'e'
           JOIN pg_extension x ON x.oid = d.refobjid AND x.extname = 'pgsodium' LOOP
    EXECUTE format('ALTER EVENT TRIGGER %I DISABLE', r.evtname);
  END LOOP;
END $$;
ALTER DATABASE noema SET pgsodium.enable_event_trigger = 'off';
REVOKE ALL ON SCHEMA pgsodium FROM PUBLIC;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA pgsodium FROM PUBLIC;
GRANT USAGE ON SCHEMA pgsodium TO noema_migrator;
GRANT EXECUTE ON FUNCTION pgsodium.crypto_sign_verify_detached(bytea, bytea, bytea) TO noema_migrator;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO noema_core;
-- Migration 0001 runs ALTER TABLE ... OWNER TO record_append, which PostgreSQL allows only if the new owner can CREATE in the schema
-- (a superuser is exempt). Held until EXE-08 (W4) transfers both Record tables to noema_migrator and revokes it, before any runtime
-- login can reach the schema; EXE-08's test 7 pins that no runtime role keeps CREATE. Debt: DEBT-05 (status/GOV-28.md).
GRANT CREATE ON SCHEMA public TO record_append;
SQL

# One login per tenant and power (DEBT-04 option A, migration 0004): <group>__<tenant>, a member of the group. The mapping rows
# (tenant_login) are added after migration by deploy/compose/postgres/provision-tenants.sh, run as the migration login. One shared
# password file serves all of them in the POC; the core holds every login in one process, so the boundary is the database's
# mapping, not the secret.
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

# One login per tenant and emitter that is not a power (DEBT-07, migration 0011): record_emitter__<tenant>__<emitter>, a member of
# record_emitter only, which holds the Record append functions and nothing else. The mapping row is added after migration by
# provision-tenants.sh through provision_emitter_login. NOEMA_EMITTERS lists the emitters; the default is core-edge, the one the edge
# needs for SESSION_VALIDATED. An emitter without a login (model-service, the adapters; OD-P-73) cannot be written.
for tenant in ${NOEMA_TENANTS:-}; do
  for emitter in ${NOEMA_EMITTERS:-core-edge}; do
    case "$emitter" in
      core-trust|core-execution|core-outcome|core-evidence|core-workflows) echo "init: '$emitter' is a power's identity" >&2; exit 1 ;;
      '') echo "init: an emitter name is empty" >&2; exit 1 ;;
      *[!a-z0-9-]*|[!a-z]*) echo "init: '$emitter' is not an emitter name (^[a-z][a-z0-9-]*\$)" >&2; exit 1 ;;
      *) ;;
    esac
    emitter_pw="$(cat "$secrets/emitter_logins/${tenant}__${emitter}")"
    psql -v ON_ERROR_STOP=1 -v role="record_emitter__${tenant}__${emitter}" -v pw="$emitter_pw" --username "$POSTGRES_USER" --dbname postgres <<'SQL'
CREATE ROLE :"role" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'pw' IN ROLE record_emitter;
SQL
  done
done
