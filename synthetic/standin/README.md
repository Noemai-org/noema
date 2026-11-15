# synthetic/standin

**Implements:** REQ-ENV-001, REQ-ENV-002, REQ-ENV-004; Option B for G1 (Founder, 4 Nov 2026). **Owner:** Integration Lead role agent. **Updated:** 2026-11-04. SYNTHETIC (AGENT-25).

The Noema-side stand-in for the Tushe-held US-01 and US-06 data. The core's Tushe-dependent adapters read it instead of Tushe until Tushe's interface is frozen; evidence from a run on it is labelled **stand-in** and says nothing about Tushe. The same scenarios rerun against live Tushe afterwards, and the adapter mapping, not this service, changes.

It is a read-store. The data contract is `benchmark/fixtures/standin.py` (`derive(fixture)`, noema#161): the benchmark loads the output once, and this service serves it. The meaning of every fixture value lives there, not here. A test-environment component, not a Noema deployable; benchmark-only on the control side (DEC-2026-10-26).

## Two sides, two credentials

| Side | Credential | Does |
|---|---|---|
| Control (`STANDIN_CONTROL_LISTEN`) | bench token (`STANDIN_BENCH_TOKEN_FILE`) | `PUT /control/v1/standin` loads or replaces the data (400 if it is not whole, 413 over 4 MiB); `GET` says what is loaded (`fixture_id`, `digest`); `DELETE` clears it |
| Data (`STANDIN_DATA_LISTEN`) | runtime token (`STANDIN_RUNTIME_TOKEN_FILE`) and a signed identity assertion (`X-Principal-Assertion`, key in `STANDIN_PRINCIPAL_KEY_FILE`) | reads only |

The two tokens and the principal key must all differ, are read from files (never the environment), and the key that loads never reads and the key that reads never loads. Neither listener defaults to every interface.

## Read routes (PERD 34.4)

All answers carry `data_source: "stand-in"`, `synthetic: true` and the header `X-Data-Source: stand-in`, errors included. There is no write route: any other method is 405 or 404 (BD-08).

| Route | PERD 34.4 operation | Answer |
|---|---|---|
| `GET /standin/v1/principal` | Identity | the caller's own accounts and beneficiaries |
| `GET /standin/v1/beneficiaries` | Beneficiary | the caller's own beneficiaries |
| `GET /standin/v1/transactions` | Candidate search | the caller's own candidates, newest first, with `complete`; filters `account`, `from`, `to` (RFC 3339), `amount_min`, `amount_max` (integers in minor units), `counterparty` |
| `GET /standin/v1/transactions/{ref}/ledger-entries` | Ledger | `fin.ledger_entry` list, or `unavailable: true` |
| `GET /standin/v1/transactions/{ref}/switch-status` | Switch | `fin.switch_record`, raw code beside semantic status, `is_terminal` |
| `GET /standin/v1/transactions/{ref}/destination-status` | Destination | `fin.destination_status` |

Never a global search. Identity is the principal bound by the caller's signed assertion (below), who must be a principal of the load (401 otherwise); a bare `X-Principal` header identifies no one. A transaction or account that belongs to someone else answers exactly as one that does not exist (404, the same body). Before anything is loaded every read is 503 `STANDIN_NOT_LOADED`, never an empty answer. `unavailable: true` is an unavailable answer, never an empty one.

## Identity (INV-12): the contract the adapter implements

The adapter derives the principal from the authenticated session and sends, with the runtime token, one header:

```
X-Principal-Assertion: v2.<base64url(principal), no padding>.<base64url(tenant), no padding>.<expiry, unix seconds>.<hex MAC>
MAC = HMAC-SHA-256 keyed with the principal key, over five lines (joined by "\n", no trailing newline):
      noema-standin-principal-v2, standin, <principal>, <tenant>, <expiry>
```

The service refuses (401) an assertion that is missing, malformed, signed with another key, bound to another audience (the audience is the literal `standin`), bound to a tenant other than the one this stand-in serves (`STANDIN_TENANT_ID`, ruling A135), in the old principal-only `v1` form, expired, valid for more than five minutes, or naming someone who is not a principal of the load. A caller holding only the runtime token cannot change the principal. Worked vector, to check an implementation: key `principal-principal-principal-principal-0003`, principal `usr_a`, expiry `1790847660` gives `v2.dXNyX2E.dGVuX2FscGhh.1790847660.36f86867759aea52907e4c76ed41bd1608534527a1f3ecbe5d7f8f57c8d9ed42` (tenant `ten_alpha`) (computed with Python `hmac`, pinned in `TestTheAssertionContractMatchesThePinnedVector`).

## Limits (stated, not hidden)

- One stand-in serves one tenant (`STANDIN_TENANT_ID`, a deployment setting, not part of the load). Every read answers with `tenant_id`, and the adapter refuses an answer for another tenant. The assertion now binds the tenant, so principal ids no longer have to be unique across tenants.
- The assertion proves that the holder of the principal key named the principal and tenant; it does not authenticate an end user by itself. The trusted adapter must mint it from the authenticated session (INV-12), and the key must be held only by that adapter. Tushe binds a principal differently; the adapter mapping absorbs that when the interface is frozen.
- Field names are the PERD 34.4 [REC] shapes, our reading, not Tushe's. Cases stay in mock-case.
- State is in memory: a restart empties it and the loader loads again. Evidence records the `digest`.
- Slice 2: Compose wiring (`deploy/compose/services/standin.yml`, profiles `ci`, `bench` and `netcheck`, never `dev` or `demo`) and the connection-test matrix rows. The hostility proxy's routes to it are in `benchmark/hostility_proxy/config/routes.standin.json` (ENV-05, 6 Nov 2026; guarded against this table by a proxy test). Still to come: a cross-language drift guard for the golden file.

## Running it

| Variable | Meaning |
|---|---|
| `STANDIN_DATA_LISTEN` | `host:port` or `:port` for the data side (the networks the container joins decide who reaches it) |
| `STANDIN_CONTROL_LISTEN` or `STANDIN_CONTROL_BIND_VIA` + `STANDIN_CONTROL_PORT` | exactly one: an explicit `host:port` naming one interface, or the local address that shares a subnet with a host known to be on the benchmark network only (Compose: `standin-anchor`) |
| `STANDIN_BENCH_TOKEN_FILE`, `STANDIN_RUNTIME_TOKEN_FILE`, `STANDIN_PRINCIPAL_KEY_FILE` | paths to the three credentials; they differ |
| `STANDIN_CLOCK_START` | optional, RFC 3339. Starts a simulated clock (see below); unset means the system clock |

`standin init-credentials -file=P ...` creates each missing credential as random hex and never replaces one; Compose runs it as `standin-credentials` into three named volumes. `standin -healthcheck` exits 0 if the data listener accepts a connection.

## The simulated clock (ENV-05, 7 Nov 2026)

With `STANDIN_CLOCK_START` set, the identity check (expiry and the five-minute limit) reads a clock that starts there and moves only when the benchmark advances it, so a run is reproducible (REQ-ENV-004, INV-21) and a client on the simulated clock is not refused as expired:

| Route (control side, bench credential) | Does |
|---|---|
| `GET /control/v1/clock` | `clock_source` `SYNTHETIC`, `simulated`, `now` |
| `POST /control/v1/clock/advance` `{"by":"PT10S"}` | moves the clock forward by an ISO 8601 time duration (`PT#H#M#S`, above zero, at most 24 hours; never backwards). `409` on the system clock |

`StandinClient` takes its clock through `StandinOptions.Now`; give it the same simulated time as the service. The real clock stays the default, so nothing changes unless the variable is set.

## Tests

`go test -race ./...` in this directory (REQ-ENV-002: credential separation; REQ-CTX-006: never a global search; REQ-ENV-004: same load, same bytes; BD-08: no write). `internal/standin/testdata/derive-golden.json` is a real `derive` output; see `testdata/README.md`.
