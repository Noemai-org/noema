# synthetic/standin

**Implements:** REQ-ENV-001, REQ-ENV-002, REQ-ENV-004; Option B for G1 (Founder, 4 Nov 2026). **Owner:** Integration Lead role agent. **Updated:** 2026-11-04. SYNTHETIC (AGENT-25).

The Noema-side stand-in for the Tushe-held US-01 and US-06 data. The core's Tushe-dependent adapters read it instead of Tushe until Tushe's interface is frozen; evidence from a run on it is labelled **stand-in** and says nothing about Tushe. The same scenarios rerun against live Tushe afterwards, and the adapter mapping, not this service, changes.

It is a read-store. The data contract is `benchmark/fixtures/standin.py` (`derive(fixture)`, noema#161): the benchmark loads the output once, and this service serves it. The meaning of every fixture value lives there, not here. A test-environment component, not a Noema deployable; benchmark-only on the control side (DEC-2026-10-26).

## Two sides, two credentials

| Side | Credential | Does |
|---|---|---|
| Control (`STANDIN_CONTROL_LISTEN`) | bench token (`STANDIN_BENCH_TOKEN_FILE`) | `PUT /control/v1/standin` loads or replaces the data (400 if it is not whole, 413 over 4 MiB); `GET` says what is loaded (`fixture_id`, `digest`); `DELETE` clears it |
| Data (`STANDIN_DATA_LISTEN`) | runtime token (`STANDIN_RUNTIME_TOKEN_FILE`) and `X-Principal` | reads only |

The two tokens must differ, are read from files (never the environment), and the key that loads never reads and the key that reads never loads. Neither listener defaults to every interface.

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

Never a global search. Identity is the caller named in `X-Principal`, who must be a principal of the load (401 otherwise). A transaction or account that belongs to someone else answers exactly as one that does not exist (404, the same body). Before anything is loaded every read is 503 `STANDIN_NOT_LOADED`, never an empty answer. `unavailable: true` is an unavailable answer, never an empty one.

## Limits (stated, not hidden)

- `X-Principal` is asserted by the caller and trusted because the runtime token is. Tushe binds the principal differently; the adapter mapping absorbs that when the interface is frozen.
- Field names are the PERD 34.4 [REC] shapes, our reading, not Tushe's. Cases stay in mock-case.
- State is in memory: a restart empties it and the loader loads again. Evidence records the `digest`.
- Slice 1: the service, its tests and its image. Compose wiring, the hostility proxy's routes to it, and the Compose profile checks follow in the next slice.

## Tests

`go test -race ./...` in this directory (REQ-ENV-002: credential separation; REQ-CTX-006: never a global search; REQ-ENV-004: same load, same bytes; BD-08: no write). `internal/standin/testdata/derive-golden.json` is a real `derive` output; see `testdata/README.md`.
