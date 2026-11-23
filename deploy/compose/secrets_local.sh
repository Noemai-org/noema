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

# The two Decision Grant HMAC key files (SEC-09): random, distinct, kept if present, never printed. A grant key is 32 random bytes in hex; a rotation adds the new key beside the old one.
for name in grant_hmac_key_a grant_hmac_key_b; do
  if [ ! -s "$dir/$name" ]; then
    python3 -c 'import secrets; print(secrets.token_hex(32))' > "$dir/$name"
  fi
  chmod 600 "$dir/$name"
done

# The core-trust signing seed (A445, interim): the Ed25519 seed of the identity `core-trust`, 32 random bytes as 64 lower-case hexadecimal characters, the form execution/signing.LoadRing reads from
# core-trust.ed25519.<version>.key (mode 0400, owned by the reading process: a one-shot step sets that, as for the grant keys). Version 1 only; a rotation adds version 2 beside it. Kept if present, never
# printed. C1's tenant-init registers the PUBLIC key in signing_key; nothing here derives or prints it.
if [ ! -s "$dir/core_trust_ed25519_seed_1" ]; then
  # umask 077 in a subshell: the file is created at 0600 and is never wider, not even before the chmod below (SR on noema#790).
  ( umask 077; python3 -c 'import secrets; print(secrets.token_hex(32))' > "$dir/core_trust_ed25519_seed_1" )
fi
chmod 600 "$dir/core_trust_ed25519_seed_1"

# The core's other key files (A506, interim): the orchestrator's (`core-workflows`) and the gateway's (`core-edge`) Ed25519 seeds and the input hash key, each 32 random bytes as 64 lower-case hexadecimal
# characters (the form the loaders read: the seeds as core-workflows.ed25519.1.key and core-edge.ed25519.1.key, the hash key as one file of at least 32 bytes), kept if present, never printed. A
# one-shot step (deploy/compose/core-keys) gives them the mode 0400 and the owner the loaders require.
if [ ! -s "$dir/core_workflows_ed25519_seed_1" ]; then
  ( umask 077; python3 -c 'import secrets; print(secrets.token_hex(32))' > "$dir/core_workflows_ed25519_seed_1" )
fi
if [ ! -s "$dir/core_edge_ed25519_seed_1" ]; then
  ( umask 077; python3 -c 'import secrets; print(secrets.token_hex(32))' > "$dir/core_edge_ed25519_seed_1" )
fi
if [ ! -s "$dir/input_hash_key" ]; then
  ( umask 077; python3 -c 'import secrets; print(secrets.token_hex(32))' > "$dir/input_hash_key" )
fi
# The Execution side's receipt signer and the Outcome verdict signer (A554, A564), each its own seed so the two never share a key: identities core-execution and core-outcome, the same form.
if [ ! -s "$dir/core_execution_ed25519_seed_1" ]; then
  ( umask 077; python3 -c 'import secrets; print(secrets.token_hex(32))' > "$dir/core_execution_ed25519_seed_1" )
fi
if [ ! -s "$dir/core_outcome_ed25519_seed_1" ]; then
  ( umask 077; python3 -c 'import secrets; print(secrets.token_hex(32))' > "$dir/core_outcome_ed25519_seed_1" )
fi
chmod 600 "$dir/core_workflows_ed25519_seed_1" "$dir/core_edge_ed25519_seed_1" "$dir/input_hash_key" "$dir/core_execution_ed25519_seed_1" "$dir/core_outcome_ed25519_seed_1"

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
