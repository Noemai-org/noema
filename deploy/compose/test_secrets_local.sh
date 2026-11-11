#!/bin/sh
# Proves secrets_local.sh makes one secret file per emitter login for the tenants and emitters Compose will give postgres: from the shell
# when set there (Compose's rule: the shell wins), otherwise from the compose folder's .env (Codex review of noema#181). Synthetic, temporary.
set -eu
here="$(cd "$(dirname "$0")" && pwd)"
fail() { echo "secrets_local.sh: $1"; exit 1; }
run() { # name, .env content (may be empty for no file); the rest of the environment is the caller's
  work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
  cp "$here/secrets_local.sh" "$work/"
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
cp "$here/secrets_local.sh" "$work/"
( cd "$work" && NOEMA_TENANTS="ten_aa ten_bb" sh secrets_local.sh >/dev/null && cp secrets/emitter_logins/ten_aa__core-edge first && NOEMA_TENANTS="ten_aa ten_bb" sh secrets_local.sh >/dev/null )
cmp -s "$work/first" "$work/secrets/emitter_logins/ten_aa__core-edge" || fail "a second run changed an existing secret"
[ "$(cat "$work"/secrets/emitter_logins/* | sort -u | wc -l)" = "2" ] || fail "two logins share a secret"
echo "secrets_local.sh: ok"
