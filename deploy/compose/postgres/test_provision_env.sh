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
echo "provision env: ok"
