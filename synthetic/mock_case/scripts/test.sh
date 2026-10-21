#!/bin/sh
# Runs the mock-case tests against a real PostgreSQL (REQ-TST-004).
#   - If MOCK_CASE_TEST_DATABASE_URL is set, it is used as it is (CI points it at a service container).
#   - Otherwise a throwaway server is started on a private socket directory and removed afterwards.
# Usage: scripts/test.sh [go test arguments]   (default: ./...)
set -eu
cd "$(dirname "$0")/.."
if [ -n "${MOCK_CASE_TEST_DATABASE_URL:-}" ]; then
  exec go test "${@:-./...}"
fi
bin="$(ls -d /usr/lib/postgresql/*/bin 2>/dev/null | sort -V | tail -1 || true)"
if [ -z "$bin" ] && command -v initdb >/dev/null 2>&1; then bin="$(dirname "$(command -v initdb)")"; fi
if [ -z "$bin" ]; then
  echo "no PostgreSQL server binaries found; set MOCK_CASE_TEST_DATABASE_URL instead" >&2
  exit 2
fi
dir="$(mktemp -d)"
port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
as_pg=""
if [ "$(id -u)" = "0" ]; then
  chown -R postgres "$dir"
  as_pg="runuser -u postgres --"
fi
cleanup() { $as_pg "$bin/pg_ctl" -D "$dir/data" -m immediate stop >/dev/null 2>&1 || true; rm -rf "$dir"; }
trap cleanup EXIT INT TERM
$as_pg "$bin/initdb" -D "$dir/data" -A trust -U postgres >/dev/null
$as_pg "$bin/pg_ctl" -D "$dir/data" -o "-p $port -k $dir -c listen_addresses=127.0.0.1 -c fsync=off -c max_connections=300" -l "$dir/log" -w start >/dev/null
MOCK_CASE_TEST_DATABASE_URL="postgres://postgres@127.0.0.1:$port/postgres?sslmode=disable"
export MOCK_CASE_TEST_DATABASE_URL
go test "${@:-./...}"
