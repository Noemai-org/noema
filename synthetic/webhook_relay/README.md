# synthetic/webhook_relay

Date: 2026-10-21. Owner task: ENV-13 (session B). Everything here is SYNTHETIC test environment, not a Noema deployable
(DEC-2026-10-16 E5).

The webhook relay of DEC-2026-10-16 E6 and REQ-API-008. Tushe delivers its webhooks to the relay; the core never
exposes a receiver. The relay verifies each delivery, stores it, and lets the core **pull** it. It is a mailbox, not an
authority: the core's Record is (INV-30).

## What it does

| Endpoint | Caller | Behaviour |
|---|---|---|
| `POST /webhooks` | Tushe | Verifies a [Standard Webhooks](https://www.standardwebhooks.com) v1 signature, stores the delivery on disk (fsync) and only then answers 200. |
| `GET /v1/deliveries?after=<seq>&limit=<n>` | the core | Verified deliveries in order, with the exact body bytes (base64). `next_after` is the cursor. Limit 1 to 500, default 100. |
| `GET /v1/stats` | the core | Counters per refusal reason, duplicates, stored total. |
| `GET /healthz` | Compose | Liveness. No data. |

Verification (`internal/verify`): headers `webhook-id`, `webhook-timestamp`, `webhook-signature`; HMAC-SHA-256 over
`<id>.<timestamp>.<body>`; one or more space-separated `v1,<base64>` entries (key rotation); timestamp within five
minutes **in either direction**; the exact body bytes as received. The check is constant-time over every entry.

Every refusal is a `400 delivery_refused` with no detail, and is counted by reason (`refused_bad_signature`,
`refused_timestamp_too_old`, `refused_timestamp_in_the_future`, `refused_missing_header`, `refused_bad_id`,
`refused_bad_timestamp`). A refused delivery is never stored.

Replays: the same `webhook-id` with the same body is acknowledged (200 `duplicate`) and stored once. The same id with a
different body is refused (409 `id_reused_with_a_different_body`) and not stored. The store is an append-only JSON-lines
file; a restart reads it again, a torn last line is cut, and a corrupt line anywhere else stops the start.

## Credentials: one secret, nothing else

The relay holds the webhook signing secret and nothing more (`WEBHOOK_RELAY_SIGNING_SECRET_FILE`, a path; the value is
never read from the environment). It **refuses to start** if its environment carries any variable that looks like a
credential (token, password, secret, key, database URL, Temporal, provider names, TLS key). It holds no TLS key and
serves plain HTTP: put the deployment's TLS terminator in front of it.

The pull token is derived from the signing secret (`HMAC-SHA-256(secret, "noema/webhook-relay/pull/v1")`, hex), so the
core can hold a credential that does not reveal the secret:

    docker compose --profile relay run --rm webhook-relay pull-token

The signing secret itself is not accepted as a pull token (tested).

## Configuration

| Variable | Meaning |
|---|---|
| `WEBHOOK_RELAY_SIGNING_SECRET_FILE` | required; `whsec_<base64>` (as Standard Webhooks writes it) or raw bytes, at least 24 bytes |
| `WEBHOOK_RELAY_STORE_FILE` | required; the JSON-lines store |
| `WEBHOOK_RELAY_LISTEN` | default `:8090` |

## Compose

`deploy/compose/services/webhook-relay.yml`, profiles `relay`, `ci`, `netcheck` (never `dev` or `demo` unless asked for
with `--profile relay`). A one-shot writes a fresh signing secret into a named volume (kept across restarts); to use the
secret Tushe was given, write it to that volume file. The port is published on `127.0.0.1:8090` by default; set
`WEBHOOK_RELAY_PUBLISH` to publish it to Tushe deliberately. The relay is on `tushe_net` (the core pulls through it)
and on `tushe_egress`, which exists only so Docker can publish the port: the relay dials nothing.

## Tests

    make env-relay-test        # gofmt, go vet, go test -race; standard library only, no database needed
    make env-relay-build       # docker build

`internal/verify` checks the documented Standard Webhooks example vector, the tolerance boundary (±300 s accepted, ±301 s
refused), key rotation and every malformed-header case. `internal/server` checks forged, tampered, unsigned, stale and
future deliveries (none stored, each counted), replay stored once, id conflict, persistence across a restart, pull
authentication and paging. `internal/config` checks the refusal on any extra credential.

## Not done here

The core's pull client, the `.env.example` entries and the documentation of the pull contract belong to the sessions
that own those paths. The sample the hostility proxy forwards for relay pulls is in `benchmark/hostility_proxy`.
