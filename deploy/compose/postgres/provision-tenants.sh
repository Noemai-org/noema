#!/bin/sh
# Adds the tenant_login mapping rows for NOEMA_TENANTS, after the migrations have run (DEBT-04 option A). Run it as the migration login (noema_migration_login, never the core's noema_core):
#   NOEMA_TENANTS="ten_aa ten_bb" PGHOST=... PGUSER=noema_migration_login PGPASSWORD=... PGDATABASE=noema deploy/compose/postgres/provision-tenants.sh
# The tenant rows themselves are created by the tenant-creation step, which must run first. It is idempotent: a login that is
# already mapped to the same tenant is left alone; a login mapped to another tenant stops the script.
set -eu
# Compose reads its .env for interpolation and does not export it to this shell, so a list set only there would be missed (Codex review of noema#181):
# read the same values the same way secrets_local.sh does. A variable set in the shell wins over .env, as in Compose. NOEMA_ENV_FILE overrides the path.
envfile="${NOEMA_ENV_FILE:-$(cd "$(dirname "$0")/.." && pwd)/.env}"
dotenv() {
  [ -f "$envfile" ] || return 0
  val="$(sed -n "s/^$1=//p" "$envfile" | tail -n 1)"
  case "$val" in
    \"*) val="${val#\"}"; val="${val%%\"*}" ;;
    \'*) val="${val#\'}"; val="${val%%\'*}" ;;
    *) val="$(printf '%s' "$val" | sed 's/[[:space:]]*#.*$//')" ;;
  esac
  printf '%s' "$val"
}
NOEMA_TENANTS="${NOEMA_TENANTS-$(dotenv NOEMA_TENANTS)}"
NOEMA_EMITTERS="${NOEMA_EMITTERS-$(dotenv NOEMA_EMITTERS)}"
echo "provision: tenants: ${NOEMA_TENANTS:-none}; emitters: ${NOEMA_EMITTERS:-core-edge}" >&2
[ -z "${PROVISION_DRY_RUN:-}" ] || exit 0
for tenant in ${NOEMA_TENANTS:-}; do
  case "$tenant" in
    ten_[0-9A-Za-z][0-9A-Za-z]*) ;;
    *) echo "provision: '$tenant' is not a tenant ID" >&2; exit 1 ;;
  esac
  for pair in trust_rw:trust execution_rw:execution outcome_rw:outcome evidence_rw:evidence orchestrator_rw:orchestrator record_read:record_read; do
    group="${pair%%:*}"; power="${pair##*:}"
    psql -v ON_ERROR_STOP=1 -v role="${group}__${tenant}" -v power="$power" -v tenant="$tenant" <<'SQL'
SELECT CASE
  WHEN EXISTS (SELECT 1 FROM tenant_login WHERE login_role = :'role' AND tenant_id = :'tenant' AND power = :'power') THEN NULL
  ELSE provision_tenant_login(:'role', :'power', :'tenant') END;
SQL
  done
done
# The logins of emitters that are not powers (DEBT-07): record_emitter__<tenant>__<emitter>, bound with provision_emitter_login.
for tenant in ${NOEMA_TENANTS:-}; do
  for emitter in ${NOEMA_EMITTERS:-core-edge}; do
    psql -v ON_ERROR_STOP=1 -v role="record_emitter__${tenant}__${emitter}" -v tenant="$tenant" -v emitter="$emitter" <<'SQL'
SELECT CASE
  WHEN EXISTS (SELECT 1 FROM tenant_login WHERE login_role = :'role' AND tenant_id = :'tenant' AND emitter = :'emitter') THEN NULL
  ELSE provision_emitter_login(:'role', :'tenant', :'emitter') END;
SQL
  done
done
