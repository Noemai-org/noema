#!/bin/sh
# Runs init.sh and assert_roles.sql against a throwaway local cluster (no Docker), then proves what the core's login can and
# cannot do by connecting as it. Needs PostgreSQL's server binaries (set PG_BIN). Synthetic passwords, a temporary directory.
set -eu
here="$(cd "$(dirname "$0")" && pwd)"
pg_bin="${PG_BIN:-$(dirname "$(command -v initdb 2>/dev/null || ls /usr/lib/postgresql/*/bin/initdb | tail -1)")}"
work="$(mktemp -d)"
trap '"$pg_bin/pg_ctl" -D "$work/data" -m immediate stop >/dev/null 2>&1 || true; rm -rf "$work"' EXIT
mkdir -p "$work/secrets"
for n in noema_core_password temporal_password noema_migrator_password noema_verify_password noema_tenant_login_password; do echo "synthetic-$n" > "$work/secrets/$n"; done
"$pg_bin/initdb" -D "$work/data" -U postgres --auth=trust >/dev/null
port=54329
"$pg_bin/pg_ctl" -D "$work/data" -o "-p $port -k $work -c listen_addresses=''" -l "$work/log" -w start >/dev/null
export PGHOST="$work" PGPORT="$port" POSTGRES_USER=postgres NOEMA_SECRETS_DIR="$work/secrets"
sh "$here/init.sh" >/dev/null
psql -v ON_ERROR_STOP=1 -X -q -U postgres -d noema -f "$here/assert_roles.sql" | grep -q "roles ok"
# As the core's login: it connects, and it creates nothing.
as_core() { PGPASSWORD="synthetic-noema_core_password" psql -X -q -tA -U noema_core -d noema -c "$1" 2>&1; }
[ "$(as_core 'select 1')" = "1" ] || { echo "noema_core cannot connect"; exit 1; }
for stmt in "create table t(i int)" "create schema s" "create temp table t(i int)" "set role noema_migrator"; do
  if as_core "$stmt" | grep -q "^ERROR"; then :; else echo "noema_core was allowed: $stmt"; exit 1; fi
done
# Every migration applies, in order, as the migration login with no CREATE ROLE right (it is a plain role).
for f in "$here"/../../../services/core/migrations/[0-9]*.sql; do
  PGPASSWORD="synthetic-noema_migrator_password" psql -v ON_ERROR_STOP=1 -X -q -U noema_migration_login -d noema -f "$f" >/dev/null
done
psql -v ON_ERROR_STOP=1 -X -q -U postgres -d noema -f "$here/assert_roles.sql" | grep -q "roles ok"
# After the last migration (EXE-08, DEBT-05): the Record tables belong to the migration role and record_append holds no CREATE.
owners="$(psql -X -q -tA -U postgres -d noema -c "select string_agg(pg_get_userbyid(relowner), ',' order by relname) from pg_class where relname in ('record_event','record_chain_head') and relkind = 'r'")"
[ "$owners" = "noema_migrator,noema_migrator" ] || { echo "Record tables are owned by: $owners"; exit 1; }
[ "$(psql -X -q -tA -U postgres -d noema -c "select has_schema_privilege('record_append','public','CREATE')")" = "f" ] || { echo "record_append still holds CREATE on public"; exit 1; }
echo "init.sh: ok"
