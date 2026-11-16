#!/bin/sh
# Proves bench_tokens.sh (EDG-01 slice 3b): it refuses a missing tenant or body limit, writes the core's session settings and the runner's issuer
# settings from the same key, never replaces a key, and prints no key material. The Go build and the issuer are stubs; everything is synthetic and temporary.
set -eu
here="$(cd "$(dirname "$0")" && pwd)"
fail() { echo "bench_tokens.sh: $1"; exit 1; }
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
mkdir -p "$work/repo/deploy/compose" "$work/repo/services/core" "$work/repo/packs/fintech/limits"
cp "$here/bench_tokens.sh" "$work/repo/deploy/compose/"
echo '{}' > "$work/repo/packs/fintech/limits/session_limits.json"
# a stub toolchain: `go build -o PATH` writes a stub issuer whose keygen makes two files
cat > "$work/go" <<'GO'
#!/bin/sh
[ "$1" = build ] && [ "$2" = -o ] || exit 1
cat > "$3" <<'IDP'
#!/bin/sh
if [ "$1" = issue ]; then shift; while [ $# -gt 0 ]; do [ "$1" = -principal ] && p="$2"; [ "$1" = -role ] && r="$2"; [ "$1" = -ttl ] && t="$2"; shift; done; echo "token-$p-$r-$t"; exit 0; fi
[ "$1" = keygen ] && [ "$2" = -dir ] || exit 1
mkdir -p "$3"; echo PRIVATE-STUB > "$3/private.pem"; echo public-stub > "$3/public.pem"
IDP
chmod +x "$3"
GO
chmod +x "$work/go"
run() { ( cd "$work/repo" && env -i PATH="$PATH" GO="$work/go" BENCH_OUT="$work/out" "$@" sh deploy/compose/bench_tokens.sh ); }
if run BENCH_MAX_BODY_BYTES=1024 >/dev/null 2>&1; then fail "ran without BENCH_TENANT"; fi
if run BENCH_TENANT=tnt_a >/dev/null 2>&1; then fail "ran without BENCH_MAX_BODY_BYTES"; fi
if run BENCH_TENANT='tnt a;x' BENCH_MAX_BODY_BYTES=1024 >/dev/null 2>&1; then fail "accepted a bad tenant"; fi
if run BENCH_TENANT=tnt_a BENCH_MAX_BODY_BYTES=0 >/dev/null 2>&1; then fail "accepted a zero body limit"; fi
if run BENCH_TENANT=tnt_a BENCH_MAX_BODY_BYTES=-1 >/dev/null 2>&1; then fail "accepted a negative body limit"; fi
msg="$(run BENCH_TENANT=tnt_a BENCH_MAX_BODY_BYTES=1024)" || fail "the script failed"
case "$msg" in *PRIVATE*) fail "key material was printed" ;; esac
idp="$work/out/idp"
grep -q "^NOEMA_SESSION_TENANTS=tnt_a$" "$idp/core.env" || fail "core.env has no tenant"
grep -q "^NOEMA_MAX_BODY_BYTES=1024$" "$idp/core.env" || fail "core.env has no body limit"
grep -q "^NOEMA_SESSION_PUBLIC_KEY_FILE=$idp/key/public.pem$" "$idp/core.env" || fail "core.env names another public key"
grep -q -- "--idp-key $idp/key/private.pem" "$idp/runner-args" || fail "runner-args names another private key"
grep -q -- "--tenant-id tnt_a" "$idp/runner-args" || fail "runner-args has no tenant"
! grep -q PRIVATE "$idp/core.env" "$idp/runner-args" || fail "a settings file holds key material"
cp "$idp/key/private.pem" "$work/first"
run BENCH_TENANT=tnt_a BENCH_MAX_BODY_BYTES=1024 >/dev/null || fail "the second run failed"
cmp -s "$work/first" "$idp/key/private.pem" || fail "a second run replaced the key"
# Tokens: only with a principal list and a lifetime, one 0400 file per principal named for it; a bad id, role or missing lifetime is refused.
[ ! -d "$idp/tokens" ] || fail "tokens were made without a principal list"
if run BENCH_TENANT=tnt_a BENCH_MAX_BODY_BYTES=1024 BENCH_PRINCIPALS="cust_1:customer" >/dev/null 2>&1; then fail "issued tokens without BENCH_TOKEN_TTL"; fi
if run BENCH_TENANT=tnt_a BENCH_MAX_BODY_BYTES=1024 BENCH_PRINCIPALS="cust_1:root" BENCH_TOKEN_TTL=10m >/dev/null 2>&1; then fail "accepted a role that does not exist"; fi
if run BENCH_TENANT=tnt_a BENCH_MAX_BODY_BYTES=1024 BENCH_PRINCIPALS="-x:ops" BENCH_TOKEN_TTL=10m >/dev/null 2>&1; then fail "accepted a principal id that reads as a flag"; fi
run BENCH_TENANT=tnt_a BENCH_MAX_BODY_BYTES=1024 BENCH_PRINCIPALS="cust_1:customer ops_1:ops" BENCH_TOKEN_TTL=10m >/dev/null || fail "the token run failed"
[ "$(cat "$idp/tokens/cust_1")" = "token-cust_1-customer-10m" ] || fail "the token of cust_1 is not for cust_1 with its role and lifetime"
[ "$(cat "$idp/tokens/ops_1")" = "token-ops_1-ops-10m" ] || fail "the token of ops_1 is not for ops_1"
[ "$(stat -c %a "$idp/tokens/cust_1")" = "400" ] || fail "a token file is not mode 0400"
echo "bench_tokens.sh: ok"
