#!/bin/sh
# Brings up the webhook relay on a public host (ENV-13; DEC-2026-10-16 E5, E6; REQ-API-008). SYNTHETIC test
# environment, not a Noema deployable (AGENT-25).
#
# What it does, in order: (1) checks its inputs, (2) points the DuckDNS name at this host, (3) starts only the relay
# from the Compose file, published on loopback, (4) starts Caddy as the TLS terminator in front of it, (5) says plainly
# whether the name answers over HTTPS.
# TLS: the relay serves plain HTTP and holds no TLS key. Caddy (decision A11, 6 Nov 2026) gets its certificate by
# HTTP-01 on ports 80 and 443 and proxies to the relay on loopback; no DNS plugin, so the DuckDNS token never reaches
# Caddy. Caddy runs on the host network (it must reach the relay's loopback port and bind 80 and 443); the relay itself
# is never published on a public interface. The script exits 3 while the name does not answer over HTTPS.
#
# Usage:
#   RELAY_HOSTNAME=<name>.duckdns.org DUCKDNS_TOKEN_FILE=/path/to/token deploy/relay-host/setup.sh [--check]
#
#   --check  validates the inputs and prints the plan; changes nothing, contacts nothing.
#
# The hostname is a variable on purpose: the Founder is reserving the name. The DuckDNS token is read from a FILE
# (never the environment, never an argument) and is sent over a curl config on stdin so it does not appear in `ps`
# or in this script's output.
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
compose_file="$here/../compose/docker-compose.yml"
project="${COMPOSE_PROJECT_NAME:-noema-relay-host}"
loopback_publish="127.0.0.1:8090"
duckdns_api="https://www.duckdns.org/update"
caddy_image="caddy:2.8.4"
caddy_container="noema-relay-caddy"
state_dir="${RELAY_HOST_STATE_DIR:-$HOME/.noema-relay-host}"
exit_terminator_absent=3

check_only=0
case "${1:-}" in
  "") ;;
  --check) check_only=1 ;;
  *) echo "usage: RELAY_HOSTNAME=... DUCKDNS_TOKEN_FILE=... $0 [--check]" >&2; exit 2 ;;
esac

fail() { echo "relay-host: $*" >&2; exit 1; }

# (1) Inputs.
hostname_value="${RELAY_HOSTNAME:-}"
[ -n "$hostname_value" ] || fail "RELAY_HOSTNAME is not set (the DuckDNS name the Founder reserves, e.g. example.duckdns.org)"
case "$hostname_value" in
  *.duckdns.org) ;;
  *) fail "RELAY_HOSTNAME '$hostname_value' is not a *.duckdns.org name" ;;
esac
subdomain="${hostname_value%.duckdns.org}"
case "$subdomain" in
  ""|*[!a-z0-9-]*) fail "RELAY_HOSTNAME '$hostname_value': the part before .duckdns.org must be lowercase letters, digits and hyphens" ;;
esac

token_file="${DUCKDNS_TOKEN_FILE:-}"
[ -n "$token_file" ] || fail "DUCKDNS_TOKEN_FILE is not set (a path to a file holding the DuckDNS token; the token itself is never accepted here)"
[ -f "$token_file" ] || fail "DUCKDNS_TOKEN_FILE '$token_file' is not a file"
[ -s "$token_file" ] || fail "DUCKDNS_TOKEN_FILE '$token_file' is empty"
[ -f "$compose_file" ] || fail "compose file not found at $compose_file"

if [ -n "${WEBHOOK_RELAY_PUBLISH:-}" ] && [ "$WEBHOOK_RELAY_PUBLISH" != "$loopback_publish" ]; then
  fail "WEBHOOK_RELAY_PUBLISH is '$WEBHOOK_RELAY_PUBLISH'; this script publishes on $loopback_publish only (the TLS terminator reaches the relay there)"
fi

echo "relay-host plan:"
echo "  hostname:   $hostname_value"
echo "  dns:        point $subdomain at this host's public address (token read from $token_file, not printed)"
echo "  relay:      docker compose project '$project', service webhook-relay, published on $loopback_publish"
echo "  tls:        Caddy ($caddy_image, container '$caddy_container', host network), HTTP-01 on ports 80 and 443, proxying to $loopback_publish"
echo "  state:      $state_dir (Caddyfile; certificates live in the Docker volume 'noema-relay-caddy-data')"
if [ "$check_only" -eq 1 ]; then
  echo "relay-host: --check, nothing changed"
  exit 0
fi

command -v docker >/dev/null 2>&1 || fail "docker is not installed"
command -v curl >/dev/null 2>&1 || fail "curl is not installed"

# (2) DuckDNS. An empty ip= makes DuckDNS use the address the request came from.
token="$(tr -d '\r\n' < "$token_file")"
answer="$(printf 'url = "%s?domains=%s&token=%s&ip="\n' "$duckdns_api" "$subdomain" "$token" \
  | curl --silent --show-error --max-time 20 --config -)" || fail "DuckDNS could not be reached"
token=""
[ "$answer" = "OK" ] || fail "DuckDNS refused the update (answered '$answer'): check the name and the token"
echo "relay-host: DuckDNS now points $hostname_value at this host"

# (3) The relay only, on loopback. The one-shot writes the signing secret into a named volume; to use the secret
# Tushe was given, write it to that volume file before starting (deploy/compose/services/webhook-relay.yml).
WEBHOOK_RELAY_PUBLISH="$loopback_publish" \
  docker compose -p "$project" -f "$compose_file" --profile relay up -d --build webhook-relay
echo "relay-host: relay started on $loopback_publish"

# (4) Caddy. The Caddyfile has one site, the hostname; Caddy obtains and renews its certificate by HTTP-01 itself.
# `docker rm -f` first so a re-run replaces the container; the certificate volume is kept.
mkdir -p "$state_dir"
printf '%s {\n\treverse_proxy 127.0.0.1:8090\n}\n' "$hostname_value" > "$state_dir/Caddyfile"
docker rm -f "$caddy_container" >/dev/null 2>&1 || true
docker run -d --name "$caddy_container" --restart unless-stopped --network host \
  -v "$state_dir/Caddyfile:/etc/caddy/Caddyfile:ro" -v noema-relay-caddy-data:/data "$caddy_image" >/dev/null
echo "relay-host: Caddy started; it asks for the certificate on first contact (allow up to a minute)"

# (5) Honest status. A 200 from /healthz over HTTPS proves the name resolves to here, Caddy holds a certificate
# and the relay answers. Retry for a minute: the first certificate is not instant.
attempt=0
while [ "$attempt" -lt 12 ]; do
  if curl --silent --max-time 10 --fail "https://$hostname_value/healthz" >/dev/null 2>&1; then
    echo "relay-host: https://$hostname_value/healthz answers; the relay is reachable by name"
    exit 0
  fi
  attempt=$((attempt + 1))
  sleep 5
done
echo "relay-host: https://$hostname_value/healthz does NOT answer after 60 s: check ports 80 and 443, the static IP and 'docker logs $caddy_container'" >&2
exit "$exit_terminator_absent"
