#!/bin/sh
# Proves secrets_local.sh makes one secret file per emitter login for the tenants and emitters Compose will give postgres: from the shell
# when set there (Compose's rule: the shell wins), otherwise from the compose folder's .env (Codex review of noema#181). Synthetic, temporary.
set -eu
here="$(cd "$(dirname "$0")" && pwd)"
fail() { echo "secrets_local.sh: $1"; exit 1; }
run() { # name, .env content (may be empty for no file); the rest of the environment is the caller's
  work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
  cp "$here/secrets_local.sh" "$here/tls_local.sh" "$work/"
  [ -z "$2" ] || printf '%s\n' "$2" > "$work/.env"
  ( cd "$work" && sh secrets_local.sh >/dev/null ) || fail "$1: the script failed"
  ls "$work/secrets/emitter_logins" | sort | tr '\n' ' '
}
unset NOEMA_TENANTS NOEMA_EMITTERS || true
[ "$(run 'no variables anywhere' '')" = "" ] || fail "files were made with no tenants"
[ "$(run 'unquoted .env' 'NOEMA_TENANTS=ten_aa ten_bb')" = "ten_aa__core-edge ten_bb__core-edge " ] || fail "unquoted .env was not read"
[ "$(run 'quoted .env with emitters' '# comment
NOEMA_TENANTS="ten_aa"
NOEMA_EMITTERS='"'"'core-edge core-record'"'"'')" = "ten_aa__core-edge ten_aa__core-record " ] || fail "quoted .env was not read"
[ "$(run 'a similar name and a comment' 'XNOEMA_TENANTS=ten_zz
#NOEMA_TENANTS=ten_yy
NOEMA_TENANTS=ten_aa # trailing')" = "ten_aa__core-edge " ] || fail "a look-alike line was read"
[ "$(NOEMA_TENANTS="ten_cc" run 'shell wins' 'NOEMA_TENANTS=ten_aa')" = "ten_cc__core-edge " ] || fail "the shell did not win over .env"
# Every file differs (a random value each), and a second run keeps the first values.
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
cp "$here/secrets_local.sh" "$here/tls_local.sh" "$work/"
( cd "$work" && NOEMA_TENANTS="ten_aa ten_bb" sh secrets_local.sh >/dev/null && cp secrets/emitter_logins/ten_aa__core-edge first && NOEMA_TENANTS="ten_aa ten_bb" sh secrets_local.sh >/dev/null )
cmp -s "$work/first" "$work/secrets/emitter_logins/ten_aa__core-edge" || fail "a second run changed an existing secret"
[ "$(cat "$work"/secrets/emitter_logins/* | sort -u | wc -l)" = "2" ] || fail "two logins share a secret"
# The two Decision Grant HMAC key files (SEC-09) are made, distinct, and kept on a second run.
for f in grant_hmac_key_a grant_hmac_key_b; do
  [ -s "$work/secrets/$f" ] || fail "secrets/$f was not made"
done
[ "$(cat "$work/secrets/grant_hmac_key_a")" != "$(cat "$work/secrets/grant_hmac_key_b")" ] || fail "the two grant keys are the same"
# Only root in the init container reads them, so they are not world-readable (mode 600).
for f in grant_hmac_key_a grant_hmac_key_b; do
  [ "$(stat -c %a "$work/secrets/$f")" = "600" ] || fail "secrets/$f is not mode 600"
done
# The core-trust signing seed (A445; LoadRing reads core-trust.ed25519.<version>.key holding the seed as 64 lower-case hexadecimal characters): made, distinct from the grant keys, mode 600 so only the
# one-shot step that will set its mode and owner reads it, and kept on a second run (a regenerated seed would orphan the public key C1's tenant-init registers).
[ -s "$work/secrets/core_trust_ed25519_seed_1" ] || fail "secrets/core_trust_ed25519_seed_1 was not made"
printf '%s' "$(cat "$work/secrets/core_trust_ed25519_seed_1")" | grep -Eq '^[0-9a-f]{64}$' || fail "the core-trust seed is not 64 lower-case hexadecimal characters"
[ "$(cat "$work/secrets/core_trust_ed25519_seed_1")" != "$(cat "$work/secrets/grant_hmac_key_a")" ] || fail "the core-trust seed is a grant key"
[ "$(stat -c %a "$work/secrets/core_trust_ed25519_seed_1")" = "600" ] || fail "secrets/core_trust_ed25519_seed_1 is not mode 600"
grep -q 'umask 077; python3 -c .*core_trust_ed25519_seed_1' "$here/secrets_local.sh" || fail "the core-trust seed is not written under umask 077 (SR on noema#790): it could be wider than 600 before the chmod"
cp "$work/secrets/core_trust_ed25519_seed_1" "$work/seed.first"
( cd "$work" && sh secrets_local.sh >/dev/null )
cmp -s "$work/seed.first" "$work/secrets/core_trust_ed25519_seed_1" || fail "a second run replaced the core-trust seed"
# The core's other key files (A506): the orchestrator's and the gateway's signing seeds and the input hash key, each 64 lower-case hexadecimal characters (the form the loaders read), mode 600 (a
# one-shot step sets 0400 and the core's owner), all different from each other and from the other keys, written under umask 077, and kept on a second run.
for f in core_workflows_ed25519_seed_1 core_edge_ed25519_seed_1 input_hash_key core_execution_ed25519_seed_1 core_outcome_ed25519_seed_1; do
  [ -s "$work/secrets/$f" ] || fail "secrets/$f was not made"
  printf '%s' "$(cat "$work/secrets/$f")" | grep -Eq '^[0-9a-f]{64}$' || fail "secrets/$f is not 64 lower-case hexadecimal characters"
  [ "$(stat -c %a "$work/secrets/$f")" = "600" ] || fail "secrets/$f is not mode 600"
  grep -q "umask 077; python3 -c .*$f" "$here/secrets_local.sh" || fail "secrets/$f is not written under umask 077"
  cp "$work/secrets/$f" "$work/$f.first"
done
[ "$(cat "$work"/secrets/core_workflows_ed25519_seed_1 "$work"/secrets/core_edge_ed25519_seed_1 "$work"/secrets/input_hash_key "$work"/secrets/core_trust_ed25519_seed_1 "$work"/secrets/grant_hmac_key_a "$work"/secrets/core_execution_ed25519_seed_1 "$work"/secrets/core_outcome_ed25519_seed_1 | sort -u | wc -l)" = "7" ] || fail "two of the core's keys are the same"
( cd "$work" && sh secrets_local.sh >/dev/null )
for f in core_workflows_ed25519_seed_1 core_edge_ed25519_seed_1 input_hash_key core_execution_ed25519_seed_1 core_outcome_ed25519_seed_1; do
  cmp -s "$work/$f.first" "$work/secrets/$f" || fail "a second run replaced secrets/$f"
done
# The mTLS material for the core and the model service (noema#555) is made too, kept on a second run, and leaves no CA key behind.
for f in ca.crt model.crt model.key core-client.crt core-client.key model-health.crt model-health.key temporal.crt temporal.key; do
  [ -s "$work/secrets/tls/$f" ] || fail "secrets/tls/$f was not made"
done
[ ! -e "$work/secrets/tls/ca.key" ] || fail "the CA's private key was left behind"
cp "$work/secrets/tls/model.key" "$work/model.key.first"
( cd "$work" && sh secrets_local.sh >/dev/null )
cmp -s "$work/model.key.first" "$work/secrets/tls/model.key" || fail "a second run replaced a complete mTLS set"
rm "$work/secrets/tls/model-health.key"
( cd "$work" && sh secrets_local.sh >/dev/null )
cmp -s "$work/model.key.first" "$work/secrets/tls/model.key" && fail "an incomplete mTLS set was not made again as a whole"
openssl verify -CAfile "$work/secrets/tls/ca.crt" "$work/secrets/tls/model.crt" "$work/secrets/tls/core-client.crt" "$work/secrets/tls/model-health.crt" >/dev/null 2>&1 || fail "the certificates are not signed by the CA"
# A relative target directory works (Codex P1 on noema#568): the files land under it, the CA key is not left behind and no work area remains.
rel="$(mktemp -d)"
( cd "$rel" && sh "$here/tls_local.sh" certs >/dev/null ) || fail "tls_local.sh failed with a relative directory"
for f in ca.crt model.crt model.key core-client.crt core-client.key model-health.crt model-health.key; do
  [ -s "$rel/certs/$f" ] || fail "a relative directory: certs/$f was not made"
done
[ ! -e "$rel/certs/ca.key" ] || fail "a relative directory: the CA's private key was left behind"
[ -z "$(find "$rel" -name 'ca.key' -o -name '.work.*' | head -n 1)" ] || fail "a relative directory: a work area or a CA key remains"
rm -rf "$rel"
echo "secrets_local.sh: ok"
