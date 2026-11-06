#!/bin/sh
# Creates local-only secret files if they are missing. The folder is git-ignored (OD-P-22).
# Values are random and never printed.
set -eu
dir="$(cd "$(dirname "$0")" && pwd)/secrets"
mkdir -p "$dir"
chmod 700 "$dir"
for name in postgres_superuser_password noema_core_password temporal_password noema_migrator_password noema_verify_password noema_tenant_login_password; do
  if [ ! -s "$dir/$name" ]; then
    python3 -c 'import secrets; print(secrets.token_hex(24))' > "$dir/$name"
  fi
  chmod 644 "$dir/$name"
done

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
