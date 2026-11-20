#!/bin/sh
# Creates the tenant rows for NOEMA_TENANTS, after the migrations have run and before provision-tenants.sh (SEC-08, migration 0030; noema#716, ruling A353).
# Run it as the operator's SUPERUSER, never as a power's login, noema_core or the migration login:
#   NOEMA_TENANTS="ten_aa ten_bb" PGHOST=... PGUSER=postgres PGDATABASE=noema deploy/compose/postgres/create-tenants.sh
# The table tenant forces row-level security and no role has an INSERT policy on it, so only a superuser, which bypasses row security, can create a tenant. The script
# refuses to run as anyone else. It takes its connection from the libpq environment (PGHOST, PGUSER, PGDATABASE and the rest of libpq's own settings):
# it sets, reads and passes no password, and no secret is ever on its command line.
# Idempotent: a tenant that already exists is left exactly as it is (its name, its time zone and a kill switch an operator has set are never rewritten).
# A new tenant starts with its kill switch off, the column default. The tenant's name is its ID until an operator renames it; SYNTHETIC in the POC.
set -eu
# Compose reads its .env for interpolation and does not export it to this shell: read it the way provision-tenants.sh does. A variable set in the shell wins. NOEMA_ENV_FILE overrides the path.
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
echo "create-tenants: tenants: ${NOEMA_TENANTS:-none}" >&2
for tenant in ${NOEMA_TENANTS:-}; do
  case "$tenant" in
    ten_[0-9A-Za-z][0-9A-Za-z]*) ;;
    *) echo "create-tenants: '$tenant' is not a tenant ID" >&2; exit 1 ;;
  esac
done
[ -z "${PROVISION_DRY_RUN:-}" ] || exit 0
[ -n "${NOEMA_TENANTS:-}" ] || exit 0
is_super="$(psql -X -q -tA -v ON_ERROR_STOP=1 -c "SELECT rolsuper FROM pg_roles WHERE rolname = current_user")"
[ "$is_super" = "t" ] || { echo "create-tenants: the connected role is not a superuser; only the operator's superuser creates a tenant (row-level security on tenant, migration 0030)" >&2; exit 1; }
for tenant in ${NOEMA_TENANTS:-}; do
  psql -X -q -v ON_ERROR_STOP=1 -v tenant="$tenant" <<'SQL'
INSERT INTO tenant (tenant_id, name) VALUES (:'tenant', :'tenant') ON CONFLICT (tenant_id) DO NOTHING;
SQL
done
