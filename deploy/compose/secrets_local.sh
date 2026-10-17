#!/bin/sh
# Creates local-only secret files if they are missing. The folder is git-ignored (OD-P-22).
# Values are random and never printed.
set -eu
dir="$(cd "$(dirname "$0")" && pwd)/secrets"
mkdir -p "$dir"
chmod 700 "$dir"
for name in postgres_superuser_password noema_core_password temporal_password; do
  if [ ! -s "$dir/$name" ]; then
    python3 -c 'import secrets; print(secrets.token_hex(24))' > "$dir/$name"
  fi
  chmod 644 "$dir/$name"
done
