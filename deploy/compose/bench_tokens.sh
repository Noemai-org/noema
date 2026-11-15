#!/bin/sh
# make bench-tokens (EDG-01 slice 3b): prepares the synthetic identity provider for a benchmark run. It builds the issuer, makes one SYNTHETIC ES256 key
# pair if there is none (a key is never replaced), and writes two files beside them: core.env, the session settings the core reads, and
# runner-args, the settings the runner reads to issue a token for each principal on demand (a token lives at most 15 minutes, so none is
# stored by default). With BENCH_PRINCIPALS (an argument, not a constant: "id:role id:role ...") and BENCH_TOKEN_TTL (a Go duration, at most 15m, no default) it also
# issues one token per principal into $BENCH_OUT/idp/tokens/<id>, mode 0400, for the runner's --tokens-dir; those files stop working when the token expires. The private key stays in a 0600 file under a 0700 directory in the git-ignored benchmark output; nothing secret is printed.
# Required, no defaults: BENCH_TENANT and BENCH_MAX_BODY_BYTES. Optional: BENCH_PRINCIPALS with BENCH_TOKEN_TTL, BENCH_OUT (benchmark/out), BENCH_AUDIENCE (noema-core), BENCH_KID (bench-k1),
# BENCH_LIMITS_FILE (the pack's session limits), GO (go). core.env is written without quotes: keep BENCH_OUT and BENCH_LIMITS_FILE free of spaces if it is sourced.
set -eu
root="$(cd "$(dirname "$0")/../.." && pwd)"
: "${BENCH_TENANT:?bench-tokens: BENCH_TENANT is required}"
: "${BENCH_MAX_BODY_BYTES:?bench-tokens: BENCH_MAX_BODY_BYTES is required}"
out="${BENCH_OUT:-$root/benchmark/out}/idp"
audience="${BENCH_AUDIENCE:-noema-core}"
kid="${BENCH_KID:-bench-k1}"
limits="${BENCH_LIMITS_FILE:-$root/packs/fintech/limits/session_limits.json}"
gocmd="${GO:-go}"
case "$BENCH_TENANT" in *[!A-Za-z0-9_]*|'') echo "bench-tokens: BENCH_TENANT is not a tenant id" >&2; exit 2 ;; esac
case "$BENCH_MAX_BODY_BYTES" in *[!0-9]*|''|0) echo "bench-tokens: BENCH_MAX_BODY_BYTES is not a positive whole number" >&2; exit 2 ;; esac
[ -f "$limits" ] || { echo "bench-tokens: no session limits file at $limits" >&2; exit 2; }
mkdir -p "$out"
chmod 700 "$out"
( cd "$root/services/core" && "$gocmd" build -o "$out/synthetic-idp" ./cmd/synthetic-idp )
if [ ! -s "$out/key/private.pem" ]; then
  "$out/synthetic-idp" keygen -dir "$out/key" >/dev/null
fi
cat > "$out/core.env" <<ENV
NOEMA_SESSION_PUBLIC_KEY_FILE=$out/key/public.pem
NOEMA_SESSION_KID=$kid
NOEMA_SESSION_AUDIENCE=$audience
NOEMA_SESSION_TENANTS=$BENCH_TENANT
NOEMA_SESSION_LIMITS_FILE=$limits
NOEMA_MAX_BODY_BYTES=$BENCH_MAX_BODY_BYTES
ENV
printf '%s\n' "--idp-key $out/key/private.pem --idp-binary $out/synthetic-idp --idp-kid $kid --idp-audience $audience --tenant-id $BENCH_TENANT" > "$out/runner-args"
if [ -n "${BENCH_PRINCIPALS:-}" ]; then
  : "${BENCH_TOKEN_TTL:?bench-tokens: BENCH_TOKEN_TTL is required with BENCH_PRINCIPALS}"
  tokens="$out/tokens"
  mkdir -p "$tokens"
  chmod 700 "$tokens"
  for pr in $BENCH_PRINCIPALS; do
    id="${pr%%:*}"; role="${pr#*:}"
    case "$id" in ''|*[!A-Za-z0-9_.:@-]*|[.:@-]*) echo "bench-tokens: a principal id is not in the form the issuer accepts" >&2; exit 2 ;; esac
    case "$role" in customer|ops|approver|audit) ;; *) echo "bench-tokens: the role of $id is not one of customer, ops, approver, audit" >&2; exit 2 ;; esac
    rm -f "$tokens/$id"
    ( umask 077; "$out/synthetic-idp" issue -key "$out/key/private.pem" -kid "$kid" -aud "$audience" -tenant "$BENCH_TENANT" -principal "$id" -role "$role" -ttl "$BENCH_TOKEN_TTL" > "$tokens/$id" )
    chmod 400 "$tokens/$id"
  done
fi
echo "bench-tokens: wrote $out/core.env and $out/runner-args (SYNTHETIC key; tokens are issued on demand by the runner)"
