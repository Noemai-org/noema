#!/bin/sh
# Runs init.sh and assert_roles.sql against a throwaway local cluster (no Docker), then proves what the core's login can and
# cannot do by connecting as it. Needs PostgreSQL's server binaries (set PG_BIN). Synthetic passwords, a temporary directory.
set -eu
here="$(cd "$(dirname "$0")" && pwd)"
pg_bin="${PG_BIN:-$(dirname "$(command -v initdb 2>/dev/null || ls /usr/lib/postgresql/*/bin/initdb | tail -1)")}"
work="$(mktemp -d)"
trap '"$pg_bin/pg_ctl" -D "$work/data" -m immediate stop >/dev/null 2>&1 || true; rm -rf "$work"' EXIT
mkdir -p "$work/secrets"
for n in noema_core_password temporal_password noema_migrator_password noema_verify_password evidence_verify_password noema_tenant_login_password; do echo "synthetic-$n" > "$work/secrets/$n"; done
# A4 (Architect ruling, 6 Nov 2026): each emitter login has its own file-mounted secret, one file per tenant and emitter.
mkdir -p "$work/secrets/emitter_logins"
echo "synthetic-emitter-ten_aa-core-edge" > "$work/secrets/emitter_logins/ten_aa__core-edge"
"$pg_bin/initdb" -D "$work/data" -U postgres --auth=trust >/dev/null
# Passwords must count: the superuser is trusted on the socket (the bootstrap), every other login must present its password.
printf 'local all postgres trust\nlocal all all scram-sha-256\n' > "$work/data/pg_hba.conf"
port=54329
"$pg_bin/pg_ctl" -D "$work/data" -o "-p $port -k $work -c listen_addresses=''" -l "$work/log" -w start >/dev/null
export PGHOST="$work" PGPORT="$port" POSTGRES_USER=postgres NOEMA_SECRETS_DIR="$work/secrets" NOEMA_TENANTS="ten_aa"
# init.sh refuses, before it creates a single role, every emitter login whose own secret is missing, empty, or equal to any other secret
# (the shared tenant-login password included). A refusal never prints a secret.
refuses() { # name; the secrets directory is "$work/bad"
  if out="$(NOEMA_SECRETS_DIR="$work/bad" sh "$here/init.sh" 2>&1)"; then echo "init.sh accepted: $1"; exit 1; fi
  case "$out" in *synthetic-*) echo "init.sh printed a secret while refusing: $1"; exit 1 ;; esac
  [ "$(psql -X -q -tA -U postgres -d postgres -c "select count(*) from pg_roles where rolname = 'noema_core'")" = "0" ] || { echo "init.sh created roles before refusing: $1"; exit 1; }
}
rm -rf "$work/bad"; cp -r "$work/secrets" "$work/bad"; rm "$work/bad/emitter_logins/ten_aa__core-edge"; refuses "a missing emitter secret"
rm -rf "$work/bad"; cp -r "$work/secrets" "$work/bad"; : > "$work/bad/emitter_logins/ten_aa__core-edge"; refuses "an empty emitter secret"
rm -rf "$work/bad"; cp -r "$work/secrets" "$work/bad"; cp "$work/bad/noema_tenant_login_password" "$work/bad/emitter_logins/ten_aa__core-edge"; refuses "an emitter secret equal to the shared tenant-login password"
rm -rf "$work/bad"; cp -r "$work/secrets" "$work/bad"; cp "$work/bad/noema_core_password" "$work/bad/emitter_logins/ten_aa__core-edge"; refuses "an emitter secret equal to the core's password"
( export NOEMA_EMITTERS="core-edge other"; rm -rf "$work/bad"; cp -r "$work/secrets" "$work/bad"
  cp "$work/bad/emitter_logins/ten_aa__core-edge" "$work/bad/emitter_logins/ten_aa__other"; refuses "two emitter logins with the same secret" )
( export NOEMA_EMITTERS="core-edge ../x"; rm -rf "$work/bad"; cp -r "$work/secrets" "$work/bad"; refuses "an emitter name that is a path (SR's note 3 on noema#181)" )
( export NOEMA_EMITTERS="core-edge Other"; rm -rf "$work/bad"; cp -r "$work/secrets" "$work/bad"; refuses "an upper-case emitter name" )
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
# The tenant logins and the emitter login are mapped by provision-tenants.sh, run as the migration login (DEBT-04, DEBT-07).
psql -X -q -U postgres -d noema -c "insert into tenant (tenant_id, name) values ('ten_aa', 'A')" >/dev/null
PGPASSWORD="synthetic-noema_migrator_password" PGUSER=noema_migration_login PGDATABASE=noema sh "$here/provision-tenants.sh" >/dev/null
mapped="$(psql -X -q -tA -U postgres -d noema -c "select count(*) from tenant_login where tenant_id = 'ten_aa'")"
[ "$mapped" = "7" ] || { echo "tenant_login rows for ten_aa: $mapped, want 7 (six tenant logins and core-edge)"; exit 1; }
as_edge() { PGPASSWORD="synthetic-emitter-ten_aa-core-edge" psql -X -q -tA -U record_emitter__ten_aa__core-edge -d noema -c "$1" 2>&1; }
case "$(as_edge 'select count(*) from record_lock_head_t()')" in 1) ;; *) echo "the core-edge login cannot lock its head"; exit 1 ;; esac
for stmt in "select * from record_event" "select * from decision_grant" "select * from execution_receipt"; do
  if as_edge "$stmt" | grep -q "^ERROR"; then :; else echo "the core-edge login was allowed: $stmt"; exit 1; fi
done
# The shared tenant-login password no longer opens an emitter login: impersonation across identities by a leaked shared secret (the security finding on noema#181).
if PGPASSWORD="synthetic-noema_tenant_login_password" psql -X -q -tA -U record_emitter__ten_aa__core-edge -d noema -c "select 1" >/dev/null 2>&1; then echo "the shared tenant-login password opens the core-edge login"; exit 1; fi
# EVD-03: the sealing path as the deployment runs it. The migration login is not a superuser here, so row-level security binds the
# SECURITY DEFINER guard of context_snapshot to its owner's policy (Security Reviewer, noema#268): an owner with no policy on
# evidence_item sees no evidence and every snapshot is refused. The Go tests migrate as a superuser and cannot show that.
as_migrator() { PGPASSWORD="synthetic-noema_migrator_password" psql -v ON_ERROR_STOP=1 -X -q -tA -U noema_migration_login -d noema -c "$1" 2>&1; }
as_login() { PGPASSWORD="synthetic-noema_tenant_login_password" psql -X -q -tA -U "$1" -d noema -c "$2" 2>&1; }
h="sha256:$(printf 'a%.0s' $(seq 1 64))"
as_login evidence_rw__ten_aa "insert into evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at) values ('pay_1', 'ten_aa', '{\"synthetic\": true}', '$h', 'S2_internal', now())" >/dev/null
as_login evidence_rw__ten_aa "insert into evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at, freshness_sla_seconds, authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at) values ('evr_aa1', 'ten_aa', 'corr_synthetic', 'fin.src.core_ledger', 'rec-1', 'TRUST', now(), now(), 60, 'NONE', 'res_s', 'prn_s', 'S2_internal', 'KNOWN', '$h', 'pay_1', now())" >/dev/null
seal() { as_login trust_rw__ten_aa "insert into context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at) values ('evs_aa1', 'ten_aa', 'corr_synthetic', array['evr_aa1'], 'rec_synthetic', 'CONSISTENT', $1, now())"; }
bad="$(seal "'$h'" || true)"
echo "$bad" | grep -q "not the hash of the stored items" || { echo "a snapshot with a wrong hash was not refused for that reason: $bad"; exit 1; }
good_hash="$(as_login trust_rw__ten_aa "select evidence_set_hash_of(array['evr_aa1'], array['$h'])")"
out="$(seal "'$good_hash'" || true)"
[ -z "$out" ] || { echo "an honest snapshot was refused under a non-superuser migration login: $out"; exit 1; }
# After 0020 (EVD-03, D9): the evidence recompute's login reads the two tables and can write, read or create nothing else.
as_ev() { PGPASSWORD="synthetic-evidence_verify_password" psql -X -q -tA -U evidence_verify -d noema -c "$1" 2>&1; }
[ "$(as_ev 'select count(*) from context_snapshot')" = "1" ] || { echo "evidence_verify cannot read context_snapshot"; exit 1; }
[ "$(as_ev 'select count(*) from evidence_item')" = "1" ] || { echo "evidence_verify cannot read evidence_item"; exit 1; }
for stmt in "delete from context_snapshot" "delete from evidence_item" "select * from evidence_payload" "select * from record_event" "select * from tenant_login" "create temp table t(i int)" "create table t(i int)" "set role trust_rw"; do
  if as_ev "$stmt" | grep -q "^ERROR"; then :; else echo "evidence_verify was allowed: $stmt"; exit 1; fi
done
# The evidence_verify secret must differ from every other login's (Security Reviewer, noema#268, non-blocking a): init.sh refuses a shared one before it touches the cluster.
mkdir "$work/secrets_dup"; cp -r "$work/secrets"/. "$work/secrets_dup"/; cp "$work/secrets/noema_verify_password" "$work/secrets_dup/evidence_verify_password"
if NOEMA_SECRETS_DIR="$work/secrets_dup" sh "$here/init.sh" 2>&1 | grep -q "evidence_verify_password"; then :; else echo "init.sh accepted an evidence_verify secret equal to another login's"; exit 1; fi
echo "init.sh: ok"
