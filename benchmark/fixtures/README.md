# benchmark/fixtures

**Implements:** REQ-BEN-001, REQ-ENV-004; Option B for G1 (Founder, 4 Nov 2026). **Owning track:** T4. **Updated:** 2026-11-04.

The fixture-loader client (DEC-2026-10-26 D2: benchmark-only, never reachable from the core) and the data contract of the **Noema-side stand-in** for the Tushe-held US-01 and US-06 data. Generated layer only: it never reads `benchmark-heldout` (INV-22, REQ-AGT-004). SYNTHETIC (AGENT-25).

## Why a stand-in

The core's adapters for ledger, switch, destination, customer and policy are paused until the Tushe OpenAPI is frozen. The Founder decided (4 Nov) that G1 runs on a Noema-side stand-in for the US-01 and US-06 data, **the evidence labelled "stand-in"**, and the same scenarios rerun against live Tushe once its OpenAPI is frozen. Nothing from a stand-in run is evidence about Tushe.

## What is here

| File | What |
|---|---|
| `standin.py` | `derive(fixture)`: a pure function from a `benchmark-fixture.v1` to the PERD 34.4 normalised payloads (`fin.ledger_entry`, `fin.switch_record`, `fin.destination_status`), the principal context and a candidate list scoped to each owner, with an explicit completeness marker. Same fixture, same bytes. An unknown status is refused, never guessed |
| `loader.py` | `StandInFixtureLoader`: derives the payloads and `PUT`s them to `PUT /control/v1/standin` on the stand-in (bench credential from a file). A refusal or silence is an unavailable loader. A fixture that already holds cases is refused until mock-case has a seed route |
| `test_standin.py` | 19 tests: each status mapping, unavailable answers, owner scoping, determinism, every fixture of the generated suite derives, the loader against a stand-in |

The runner uses it with `--standin-url URL --bench-credential-file FILE`.

## How a fixture value is read (the stand-in's meaning, written once)

| Fixture field | Value | Stand-in answer |
|---|---|---|
| `ledger` | POSTED | one `DEBIT` (outgoing) or `CREDIT` (incoming), `POSTED` |
| | NOT_POSTED | an answer with no entry (`entries: []`, `unavailable: false`) |
| | REVERSED | the original entry `REVERSED` and a `REVERSAL` entry `POSTED` |
| | UNAVAILABLE | an unavailable answer (not an empty one) |
| `switch` | APPROVED / DECLINED / TIMED_OUT / PENDING | `semantic_status` ACCEPTED / REJECTED / TIMED_OUT / PENDING; terminal for the first two only; a `SYN-` raw code kept beside it, labelled SYNTHETIC |
| `destination`, `settlement` | the fixture's value | `credit_state`, `settlement_state`, with `effective_at` (clock start minus `age_minutes`) and `observed_at` (clock start) |
| `age_minutes` | n | `occurred_at` = the simulated clock start `2026-10-01T09:40:00+01:00` minus n minutes |

## What the stand-in service needs (for the Integration Lead)

A read-store on `bench_net`/the adapters' network with one control route, `PUT /control/v1/standin` (bench credential), that replaces its contents with the body of `derive(fixture)`, and read routes for the PERD 34.4 operations that return these payloads scoped to the authenticated principal (never a global search). The route name and body are this repository's proposal; the service is not built here. Cases stay in mock-case.

## Limits (stated, not hidden)

- The field names are the PERD 34.4 `[REC]` shapes, fixed when the Tushe OpenAPI is frozen; the stand-in is our reading, not Tushe's. When Tushe's interface is frozen the adapter mapping, not this file, changes, and the scenarios rerun.
- Existing cases in a fixture cannot be seeded (mock-case has no seed route); none of the G1 cases has any.
