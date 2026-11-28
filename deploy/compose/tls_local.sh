#!/bin/sh
# Creates the local mTLS material between the core and the model service (PERD 36.1, AGENTS section 6: mTLS between the two services, never a shared bearer; noema#555).
# Usage: tls_local.sh [directory]   (default: secrets/tls next to this script; the folder is git-ignored, OD-P-22). SYNTHETIC throwaway material for a local or CI stack only.
#
# One local CA signs three certificates, each with its own key:
#   model.crt/.key         the model service's server certificate (DNS model, localhost; IP 127.0.0.1). Mounted into the model only.
#   core-client.crt/.key   the core's client certificate (the peer identity the model accepts). Mounted into the core only.
#   model-health.crt/.key  the model's own health-check client certificate, so the core's key never sits in the model container. Mounted into the model only.
#   ca.crt                 the CA certificate, mounted into both.
#   ops.crt/.key           the ops listener's server certificate (A775; DNS ops, localhost; IP 127.0.0.1), signed by the OPERATOR CA. Mounted into the ops service only.
#   operator-client.crt/.key  one SYNTHETIC operator's client certificate (CN op_synthetic), signed by the OPERATOR CA, for the stack-check and tests; a real operator brings their own.
#   operator-ca.crt        a SECOND, separate CA (A721): its own key and subject, signs no service certificate here. The ops listener trusts this one for operator client certificates and the
#                          service CA never, so a service certificate can never act as an operator. Its private key is deleted with the service CA's: operator certificates are issued by whoever holds an operator CA key.
# The CA's private key exists only while this script runs and is deleted before it ends: nothing can mint a further certificate afterwards. If any of the seven files is
# missing or empty, ALL are made again (a partial set would pair keys with certificates of different CAs). Existing complete sets are kept. Values are never printed.
set -eu
# Git Bash on Windows rewrites an argument that starts with / (such as -subj /CN=x) into a Windows path; these two variables stop that and mean nothing elsewhere.
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL="*"
here="$(cd "$(dirname "$0")" && pwd)"
dir="${1:-$here/secrets/tls}"
# An absolute path, made before anything changes directory: the work area below is entered with cd, and a relative target would then point inside it (Codex P1 on noema#568: the
# copy failed and the trap left the CA's private key behind in the work area).
mkdir -p "$dir"
dir="$(cd "$dir" && pwd)"
files="ca.crt operator-ca.crt ops.crt ops.key operator-client.crt operator-client.key model.crt model.key core-client.crt core-client.key model-health.crt model-health.key temporal.crt temporal.key"
complete=1
for f in $files; do
  [ -s "$dir/$f" ] || complete=0
done
if [ "$complete" -eq 1 ]; then
  exit 0
fi
command -v openssl >/dev/null || { echo "tls_local.sh: openssl is required to make the local mTLS material" >&2; exit 2; }
umask 077
# The directory can be entered but not listed by other users (the containers run as unprivileged users that are not the owner and read the files by name).
chmod 711 "$dir"
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

# The operator CA (A721): a different key and a different name from the service CA above; it signs only the two operator-side certificates below (the ops listener's own server certificate and one synthetic operator's client certificate), never a service certificate.
ssl req -x509 -newkey rsa:2048 -nodes -keyout operator-ca.key -out operator-ca.crt -days 365 -subj "/CN=noema-poc-local-operator-ca" -addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign,cRLSign"

# issue NAME CN EXTENDED-KEY-USAGE SAN-or-empty [CA-PREFIX]  (CA-PREFIX defaults to ca: the service CA; "operator-ca" signs with the operator CA)
issue() {
  name="$1"; cn="$2"; eku="$3"; san="$4"; signer="${5:-ca}"
  { printf 'basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=%s
' "$eku"; [ -z "$san" ] || printf 'subjectAltName=%s
' "$san"; } > "$name.ext"
  ssl req -newkey rsa:2048 -nodes -keyout "$name.key" -out "$name.csr" -subj "/CN=$cn"
  ssl x509 -req -in "$name.csr" -CA "$signer.crt" -CAkey "$signer.key" -CAcreateserial -out "$name.crt" -days 365 -extfile "$name.ext"
}
issue model model serverAuth "DNS:model,DNS:localhost,IP:127.0.0.1"
issue core-client core clientAuth ""
issue model-health model-health clientAuth ""
# The Temporal frontend's server certificate (A296): the name the clients verify is "temporal"; localhost and the loopback address are there for the container's own healthcheck.
issue temporal temporal serverAuth "DNS:temporal,DNS:localhost,IP:127.0.0.1"
# The operator side (A775), signed by the operator CA only. `ops` is the name an operator verifies; op_synthetic is the one synthetic operator (its common name becomes the principal, A721).
issue ops ops serverAuth "DNS:ops,DNS:localhost,IP:127.0.0.1" operator-ca
issue operator-client op_synthetic clientAuth "" operator-ca

cp ca.crt "$dir/ca.crt"
cp operator-ca.crt "$dir/operator-ca.crt"
for n in model core-client model-health temporal ops operator-client; do
  cp "$n.crt" "$dir/$n.crt"
  cp "$n.key" "$dir/$n.key"
done
cd "$dir"
# The containers run as unprivileged users (uid 10001 in the model image) that are not the owner of these bind-mounted files, so the keys must be readable by others, like the other
# local secret files (OD-P-22). That is the known limit of a bind mount: the folder is git-ignored, cannot be listed by other users (mode 711), and holds throwaway SYNTHETIC material
# for a local or CI stack whose CA key is deleted above. A mode of 0600 would need the containers to run as the invoking user, or a one-shot step that writes into a volume owned
# by them; neither is built here (noema#568, Security Reviewer's note c).
chmod 644 "$dir"/*.crt "$dir"/*.key
