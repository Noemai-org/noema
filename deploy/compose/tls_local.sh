#!/bin/sh
# Creates the local mTLS material between the core and the model service (PERD 36.1, AGENTS section 6: mTLS between the two services, never a shared bearer; noema#555).
# Usage: tls_local.sh [directory]   (default: secrets/tls next to this script; the folder is git-ignored, OD-P-22). SYNTHETIC throwaway material for a local or CI stack only.
#
# One local CA signs three certificates, each with its own key:
#   model.crt/.key         the model service's server certificate (DNS model, localhost; IP 127.0.0.1). Mounted into the model only.
#   core-client.crt/.key   the core's client certificate (the peer identity the model accepts). Mounted into the core only.
#   model-health.crt/.key  the model's own health-check client certificate, so the core's key never sits in the model container. Mounted into the model only.
#   ca.crt                 the CA certificate, mounted into both.
# The CA's private key exists only while this script runs and is deleted before it ends: nothing can mint a further certificate afterwards. If any of the seven files is
# missing or empty, ALL are made again (a partial set would pair keys with certificates of different CAs). Existing complete sets are kept. Values are never printed.
set -eu
# Git Bash on Windows rewrites an argument that starts with / (such as -subj /CN=x) into a Windows path; these two variables stop that and mean nothing elsewhere.
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL="*"
here="$(cd "$(dirname "$0")" && pwd)"
dir="${1:-$here/secrets/tls}"
files="ca.crt model.crt model.key core-client.crt core-client.key model-health.crt model-health.key"
complete=1
for f in $files; do
  [ -s "$dir/$f" ] || complete=0
done
if [ "$complete" -eq 1 ]; then
  exit 0
fi
command -v openssl >/dev/null || { echo "tls_local.sh: openssl is required to make the local mTLS material" >&2; exit 2; }
umask 077
mkdir -p "$dir"
chmod 755 "$dir"
rm -f $(for f in $files; do printf '%s ' "$dir/$f"; done)
# The work area is inside the target directory and openssl is given only relative names from there, so no path is ever translated between a POSIX shell and a native openssl.
work="$dir/.work.$$"
mkdir -p "$work"
trap 'rm -rf "$work"' EXIT INT TERM
cd "$work"

# openssl prints key-generation progress on stderr; keep it out of the log and show it only if a command fails.
ssl() {
  openssl "$@" >/dev/null 2>err || { cat err >&2; exit 1; }
}
ssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.crt -days 365 -subj "/CN=noema-poc-local-ca"   -addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign,cRLSign"

# issue NAME CN EXTENDED-KEY-USAGE SAN-or-empty
issue() {
  name="$1"; cn="$2"; eku="$3"; san="$4"
  { printf 'basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=%s
' "$eku"; [ -z "$san" ] || printf 'subjectAltName=%s
' "$san"; } > "$name.ext"
  ssl req -newkey rsa:2048 -nodes -keyout "$name.key" -out "$name.csr" -subj "/CN=$cn"
  ssl x509 -req -in "$name.csr" -CA ca.crt -CAkey ca.key -CAcreateserial -out "$name.crt" -days 365 -extfile "$name.ext"
}
issue model model serverAuth "DNS:model,DNS:localhost,IP:127.0.0.1"
issue core-client core clientAuth ""
issue model-health model-health clientAuth ""

cp ca.crt "$dir/ca.crt"
for n in model core-client model-health; do
  cp "$n.crt" "$dir/$n.crt"
  cp "$n.key" "$dir/$n.key"
done
cd "$dir"
# The containers run as unprivileged users that are not the owner of these bind-mounted files, so they are readable, like the other local secret files (OD-P-22): the folder is git-ignored
# and holds throwaway synthetic material.
chmod 644 "$dir"/*.crt "$dir"/*.key
