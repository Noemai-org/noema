#!/bin/sh
# A721 (SR's note on A699): the operator CA is a different CA from the service-to-service mTLS CA, so a service certificate can never act as an operator. This runs tls_local.sh into a
# scratch folder and checks, on the real files, that the two CAs differ in every way that matters: the file, the subject, the public key, and that neither CA's private key is left behind.
# Needs openssl. Prints no key. SYNTHETIC throwaway material only.
set -eu
here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM
fail() { echo "test_tls_local.sh: $1" >&2; exit 1; }
sh "$here/tls_local.sh" "$work/tls" >/dev/null || fail "tls_local.sh failed"
[ -s "$work/tls/ca.crt" ] || fail "the service CA certificate is missing"
[ -s "$work/tls/operator-ca.crt" ] || fail "the operator CA certificate is missing"
cmp -s "$work/tls/ca.crt" "$work/tls/operator-ca.crt" && fail "the operator CA is the service CA file"
subj_service="$(openssl x509 -in "$work/tls/ca.crt" -noout -subject)"
subj_operator="$(openssl x509 -in "$work/tls/operator-ca.crt" -noout -subject)"
[ "$subj_service" != "$subj_operator" ] || fail "the two CAs have the same subject"
key_service="$(openssl x509 -in "$work/tls/ca.crt" -noout -pubkey | openssl dgst -sha256)"
key_operator="$(openssl x509 -in "$work/tls/operator-ca.crt" -noout -pubkey | openssl dgst -sha256)"
[ "$key_service" != "$key_operator" ] || fail "the two CAs have the same key"
# no certificate the service CA signed verifies against the operator CA (a service certificate is never an operator)
for c in model core-client model-health temporal; do
  if openssl verify -CAfile "$work/tls/operator-ca.crt" "$work/tls/$c.crt" >/dev/null 2>&1; then fail "the service certificate $c verifies against the operator CA"; fi
  openssl verify -CAfile "$work/tls/ca.crt" "$work/tls/$c.crt" >/dev/null 2>&1 || fail "the service certificate $c no longer verifies against the service CA"
done
# the operator CA is a CA, and neither CA's private key is kept (nothing can mint a further certificate afterwards)
openssl x509 -in "$work/tls/operator-ca.crt" -noout -text | grep -q "CA:TRUE" || fail "the operator CA certificate is not a CA"
for f in ca.key operator-ca.key; do [ ! -e "$work/tls/$f" ] || fail "$f was left behind"; done
ls -A "$work/tls" | grep -q '^\.work' && fail "the work area was left behind"
# a second run keeps the complete set as it is
before="$(cksum "$work/tls/operator-ca.crt")"
sh "$here/tls_local.sh" "$work/tls" >/dev/null
[ "$before" = "$(cksum "$work/tls/operator-ca.crt")" ] || fail "a second run changed the operator CA"
echo "test_tls_local.sh: ok"
