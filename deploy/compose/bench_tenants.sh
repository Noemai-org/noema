#!/bin/sh
# The ONE effective tenant list of the bench stack (Codex P2 on noema#790; A356). `make bench-up` passes it to secrets_local.sh and to Compose, so the secrets the stack makes, the logins postgres
# creates and the DSN files trust-dsn-init writes are for the same tenants. Resolution, in this order, by the rules secrets_local.sh and provision-tenants.sh already use:
#   1. NOEMA_TENANTS in the shell (the shell wins over .env, as in Compose); 2. NOEMA_TENANTS in Compose's .env (deploy/compose/.env, or NOEMA_ENV_FILE); 3. the stand-in tenant given as the argument.
# A list that is empty after 1 and 2 is no list: the argument is used. Prints the list and nothing else; reads no secret.
set -eu
fallback="${1-}"
envfile="${NOEMA_ENV_FILE:-$(cd "$(dirname "$0")" && pwd)/.env}"
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
list="${NOEMA_TENANTS-$(dotenv NOEMA_TENANTS)}"
[ -n "$list" ] || list="$fallback"
printf '%s\n' "$list"
