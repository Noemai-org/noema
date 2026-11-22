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
# Make pastes the printed list into a recipe's command line as TEXT, so every word is checked against the tenant shape before anything is printed (SR and C1 on noema#790): a quote, a `$(...)`
# or a `;` in a .env value must stop here, never reach a shell. The stand-in tenant must be one of the tenants: sessions name it, and a tenant with no login cannot be served (C1's P3).
tenant_ok() {
  case "$1" in
    ''|*[!a-z0-9_]*) return 1 ;;  # ^[a-z0-9_]+$ first (A469 to A485, item 6): nothing else ever reaches a recipe's text
  esac
  case "$1" in
    ten_[0-9A-Za-z][0-9A-Za-z]*) ;;
    *) return 1 ;;
  esac
  case "$1" in
    *[!0-9A-Za-z_]*) return 1 ;;
  esac
  [ "${#1}" -ge 6 ] && [ "${#1}" -le 36 ]
}
bad() { echo "bench-tenants: $1" >&2; exit 1; }
tenant_ok "$fallback" || bad "the stand-in tenant is not a tenant id (ten_ and 2 to 32 letters or digits)"
found=0
for word in $list; do
  tenant_ok "$word" || bad "a tenant in the list is not a tenant id (ten_ and 2 to 32 letters or digits)"
  [ "$word" != "$fallback" ] || found=1
done
[ "$found" -eq 1 ] || bad "the stand-in tenant is not one of the tenants: sessions would name a tenant with no login"
printf '%s\n' "$list"
