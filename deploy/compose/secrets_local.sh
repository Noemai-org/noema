#!/bin/sh
# Creates local-only secret files if they are missing. The folder is git-ignored (OD-P-22).
# Values are random and never printed.
set -eu
here="$(cd "$(dirname "$0")" && pwd)"
dir="$here/secrets"
mkdir -p "$dir"
chmod 700 "$dir"
for name in postgres_superuser_password noema_core_password temporal_password noema_migrator_password noema_verify_password evidence_verify_password noema_tenant_login_password; do
  if [ ! -s "$dir/$name" ]; then
    python3 -c 'import secrets; print(secrets.token_hex(24))' > "$dir/$name"
  fi
  chmod 644 "$dir/$name"
done

# The value of NAME in the .env next to this script, by Compose's rules: KEY=value, matching quotes removed, a " #" comment ends an unquoted value.
# The last line wins. Nothing in the file is executed.
dotenv() {
  [ -f "$here/.env" ] || return 0
  val="$(sed -n "s/^$1=//p" "$here/.env" | tail -n 1)"
  case "$val" in
    \"*) val="${val#\"}"; val="${val%%\"*}" ;;
    \'*) val="${val#\'}"; val="${val%%\'*}" ;;
    *) val="$(printf '%s' "$val" | sed 's/[[:space:]]*#.*$//')" ;;
  esac
  printf '%s' "$val"
}
# Compose's rule: a variable set in the shell wins over .env. These are the values Compose will give the postgres service.
NOEMA_TENANTS="${NOEMA_TENANTS-$(dotenv NOEMA_TENANTS)}"
NOEMA_EMITTERS="${NOEMA_EMITTERS-$(dotenv NOEMA_EMITTERS)}"

# One secret file per emitter login (ruling A4): NOEMA_TENANTS x NOEMA_EMITTERS (default core-edge). Missing files are made with a random value;
# existing files are kept. The folder is mounted read-only into postgres only; init.sh refuses a missing, empty or duplicated secret.
mkdir -p "$dir/emitter_logins"
chmod 755 "$dir/emitter_logins"
for tenant in ${NOEMA_TENANTS:-}; do
  for emitter in ${NOEMA_EMITTERS:-core-edge}; do
    f="$dir/emitter_logins/${tenant}__${emitter}"
    if [ ! -s "$f" ]; then
      python3 -c 'import secrets; print(secrets.token_hex(24))' > "$f"
    fi
    chmod 644 "$f"
  done
done

# The local mTLS material between the core and the model service (noema#555): a throwaway CA, the model's server certificate and the core's client certificate. Kept if complete.
sh "$here/tls_local.sh"
