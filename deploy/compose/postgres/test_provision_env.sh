#!/bin/sh
# The provisioning script reads NOEMA_TENANTS and NOEMA_EMITTERS from the compose .env when the shell does not set them (Codex review of noema#181). SYNTHETIC.
set -eu
here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
printf 'NOEMA_TENANTS="ten_aa ten_bb"\nNOEMA_EMITTERS=core-edge other # comment\n' > "$work/env"
out="$(unset NOEMA_TENANTS NOEMA_EMITTERS; NOEMA_ENV_FILE="$work/env" PROVISION_DRY_RUN=1 sh "$here/provision-tenants.sh" 2>&1)"
[ "$out" = "provision: tenants: ten_aa ten_bb; emitters: core-edge other" ] || { echo "from .env: got '$out'" >&2; exit 1; }
out="$(NOEMA_TENANTS=ten_cc NOEMA_EMITTERS=core-edge NOEMA_ENV_FILE="$work/env" PROVISION_DRY_RUN=1 sh "$here/provision-tenants.sh" 2>&1)"
[ "$out" = "provision: tenants: ten_cc; emitters: core-edge" ] || { echo "shell wins: got '$out'" >&2; exit 1; }
out="$(unset NOEMA_TENANTS NOEMA_EMITTERS; NOEMA_ENV_FILE="$work/none" PROVISION_DRY_RUN=1 sh "$here/provision-tenants.sh" 2>&1)"
[ "$out" = "provision: tenants: none; emitters: core-edge" ] || { echo "no .env: got '$out'" >&2; exit 1; }
# SEC-08 (noema#716 ruling A353): create-tenants.sh, run by the operator's superuser, reads the same two settings the same way and says what it would do.
out="$(unset NOEMA_TENANTS; NOEMA_ENV_FILE="$work/env" PROVISION_DRY_RUN=1 sh "$here/create-tenants.sh" 2>&1)"
[ "$out" = "create-tenants: tenants: ten_aa ten_bb" ] || { echo "create-tenants from .env: got '$out'" >&2; exit 1; }
out="$(NOEMA_TENANTS=ten_cc NOEMA_ENV_FILE="$work/env" PROVISION_DRY_RUN=1 sh "$here/create-tenants.sh" 2>&1)"
[ "$out" = "create-tenants: tenants: ten_cc" ] || { echo "create-tenants, shell wins: got '$out'" >&2; exit 1; }
if out="$(NOEMA_TENANTS='ten_aa x' NOEMA_ENV_FILE="$work/none" PROVISION_DRY_RUN=1 sh "$here/create-tenants.sh" 2>&1)"; then echo "create-tenants accepted 'x' as a tenant ID" >&2; exit 1; fi
# No secret on a command line: the script takes its connection from the libpq environment and never sets or passes a password.
if grep -nE 'PGPASSWORD|PGPASSFILE|--password|-W ' "$here/create-tenants.sh"; then echo "create-tenants.sh handles a password itself" >&2; exit 1; fi
# Nothing but create-tenants.sh creates a tenant row outside the tests (tenant forces row-level security with no INSERT policy, migration 0030): a second creator would be a second path.
root="$(cd "$here/../../.." && pwd)"
found="$(cd "$root" && grep -rIliE 'insert[[:space:]]+into[[:space:]]+(public\.)?tenant([[:space:]]|\()' --exclude-dir=.git --exclude-dir=node_modules --exclude='*_test.go' --exclude='test_*.sh' --exclude='*.md' . | sort)"
[ "$found" = "./deploy/compose/postgres/create-tenants.sh" ] || { echo "files that create a tenant row: '$found'" >&2; exit 1; }
echo "provision env: ok"
