#!/bin/sh
# Runs once, on first start of an empty mock-case-db data volume (ENV-08). mock-case has its own database, its own
# roles and its own credentials: nothing here is shared with the core's Postgres. SYNTHETIC test environment.
#   mock_case_admin  owns the database and runs migrations and the bench-only reset
#   mock_case_app    serves the case API: no DDL, and the tables refuse the rest (migrations/0001_init.sql)
set -eu
dir="${MOCK_CASE_DB_CREDENTIALS_DIR:-/run/mock-case-db}"
app_pw="$(cat "$dir/app_password")"
admin_pw="$(cat "$dir/admin_password")"
psql -v ON_ERROR_STOP=1 -v app_pw="$app_pw" -v admin_pw="$admin_pw" --username "$POSTGRES_USER" --dbname postgres <<'SQL'
CREATE ROLE mock_case_admin LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'admin_pw';
CREATE ROLE mock_case_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'app_pw';
CREATE DATABASE mock_case OWNER mock_case_admin;
REVOKE ALL ON DATABASE mock_case FROM PUBLIC;
GRANT CONNECT ON DATABASE mock_case TO mock_case_app;
SQL
