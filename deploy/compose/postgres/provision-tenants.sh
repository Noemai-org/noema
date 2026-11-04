#!/bin/sh
# Adds the tenant_login mapping rows for NOEMA_TENANTS, after the migrations have run (DEBT-04 option A). Run it as the migration login (noema_migration_login, never the core's noema_core):
#   NOEMA_TENANTS="ten_aa ten_bb" PGHOST=... PGUSER=noema_migration_login PGPASSWORD=... PGDATABASE=noema deploy/compose/postgres/provision-tenants.sh
# The tenant rows themselves are created by the tenant-creation step, which must run first. It is idempotent: a login that is
# already mapped to the same tenant is left alone; a login mapped to another tenant stops the script.
set -eu
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
