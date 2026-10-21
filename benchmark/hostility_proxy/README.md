# benchmark/hostility_proxy

**Implements:** REQ-ENV-005 (task ENV-09), REQ-ENV-004, REQ-ENV-006, REQ-ENV-002; PERD §34.4 (control plane), §37.1 (FaultPlan, `source: PROXY`), §65.1.
**Owning track:** T4 item 7 (DEC-2026-10-20), session B. `bench` and `ci` profiles only. Fault plans are owned by the benchmark owner (DEC-2026-10-16 E2, point 4).
**Status:** built. SYNTHETIC: every number it fabricates, every fixture and every injected string is labelled so in the files that define it.

The hostility proxy sits between the system under test and the hosted Tushe sandbox (and the core's pull from the webhook relay). Tushe produces no transport fault, no stale read and no event-delivery fault, and refuses to fake impossible state; the proxy is the primary source for those families, and it rewrites Tushe's test account numbers so the environment still forces a real investigation (DEC-2026-10-12 D4). It is a separate Go module with its own Dockerfile, built on the standard library only. It holds no decision, grant, Record or provider credential and runs nowhere else than the bench, ci and test profiles: it refuses to start unless `HOSTILITY_PROXY_PROFILE` says so, and Compose lists it in no other profile.

## Two sides

| Side | Where (PERD §65.1) | Opens for | What |
|---|---|---|---|
| data plane `:8080` | `tushe_net` | no credential of its own: Tushe's credentials pass through and Tushe checks them | forwards the requests of the route table, and nothing else |
| control API `:8081` | `bench_net` **only** | the **bench** credential | `/control/v1/...` |

The control listener is bound to the one local address that shares a subnet with `HOSTILITY_PROXY_CONTROL_BIND_VIA` (a host on `bench_net` only), so a caller on `tushe_net` cannot connect at all, and if that address cannot be found the proxy does not start. Every request without the bench credential is refused with 401 and logged. The data plane has no control route: `/control/...` on it is 404.

**Not an open relay.** `config/routes.json` lists the method and path of each forwarded request, the upstream it goes to, the logical service and operation a fault plan addresses it by, and the request attributes a fault's `match` may use (`path:<var>`, `query:<name>`, `header:<Name>`). Anything not in the table, and every HEAD, is a 404 that never reaches an upstream. Upstream URLs come from `HOSTILITY_PROXY_UPSTREAM_<NAME>`, one per upstream the table names. The sample table is SYNTHETIC and follows no real Tushe interface; replace it when the Tushe OpenAPI is frozen (REQ-ENV-001).

## Faults

A plan (`contracts/schemas/fault-plan.v1.json`) is loaded with `PUT /control/v1/fault-plan`. The proxy applies the faults of a plan whose `source` is `PROXY` and whose `target` is not `mock-case`, and counts every other fault as ignored (`applied_faults`, `ignored_faults`): they are Tushe's or mock-case's. It refuses (422) a PROXY plan it cannot apply exactly: an effect it does not produce, a target and operation with no route, a match key the route cannot supply, a missing fixture, a bad param. Nothing is silently skipped.

| Effect | Params | What the system under test observes |
|---|---|---|
| `LATENCY`, `NEAR_TIMEOUT_BEFORE`, `NEAR_TIMEOUT_AFTER` | `delay_ms` | the real answer, `delay_ms` late (set it just under or just over the caller's timeout) |
| `TIMEOUT_BEFORE_COMMIT` | `delay_ms` | nothing reaches Tushe; the connection is held `delay_ms`, then dropped |
| `TIMEOUT_AFTER_COMMIT` | `delay_ms` | the request reaches Tushe; the answer is held `delay_ms`, then the connection is dropped |
| `DROP_RESPONSE` | none | the request reaches Tushe; the connection is dropped at once |
| `HTTP_5XX` | `status` (500 to 599), `after_commit` | a 5xx; with `after_commit` the request reached Tushe first |
| `HTTP_429` | `retry_after_seconds` (optional) | 429, with `Retry-After` only if given; nothing reaches Tushe |
| `SERVICE_UNAVAILABLE` | none | 503; nothing reaches Tushe |
| `MALFORMED_RESPONSE` | none | status and headers as they were, the first half of the body |
| `STALE_READ` | `seconds` | a GET answered with the newest response the proxy itself saw at least `seconds` ago (on the plan's clock); if none is that old, the live one, and the trace says `live` |
| `SCHEMA_DRIFT` | `change` (`add`, `rename`, `remove`, `unknown_enum`), `field` | the JSON changed the way a silent producer change would |
| `CLOCK_SKEW` | `skew_seconds` | every RFC 3339 timestamp in the body shifted, offset and fraction kept |
| `INJECTION_PAYLOAD` | `field` | the string value of `field` replaced by an instruction-like string, each labelled `[SYNTHETIC INJECTION TEST]` in its text |
| `IMPOSSIBLE_STATE` | `fixture` | a response from `config/fixtures/<fixture>.json` in place of the real one; Tushe is never asked. A fixture file must say `"synthetic": true` and that label is not sent |
| `DROP_WEBHOOK`, `DUPLICATE_WEBHOOK`, `REORDER_EVENTS`, `LATE_EVENT` | none (`delay_ms` for late) | on an event-feed route (and the core's pull from the relay): the first event missing, doubled, one adjacent pair swapped (the seed picks which), or the events withheld and delivered `delay_ms` later in a response of the same feed, after the events that followed them |

`match` works as in mock-case: `nth_call` counts the calls that also satisfy the other keys, the first matching fault in plan order applies, and `probability` is a seeded draw (0 and 1 are exact). Plans are written with Tushe's own values; the proxy maps the request's synthetic numbers back before it matches. **A fault never marks a response**: no header, no field, no body text names the proxy (`TestFaultedResponsesCarryNoMarker`). Faults are visible only in the trace.

`clock.start` starts a simulated clock (used for `STALE_READ` and `LATE_EVENT`); `POST /control/v1/clock/advance {"step": "..."}` or `{"by": "PT10S"}` moves it.

Reproduction (REQ-ENV-004): `seed` feeds every choice the proxy makes. The same plan and seed on the simulated clock give an identical trace and identical answers (`GET /control/v1/trace` returns `trace_hash`); a different seed changes what the seed chooses (`TestReplayFromTheSameSeedIsIdentical`).

## Test account numbers

Tushe's test accounts are `9999SSMMMM` (REQ-ENV-005). A system that saw them could learn that `999904...` means "will be reversed", so the proxy rewrites every ten-digit run starting 9999 in a response (JSON strings and keys, text bodies, `Location`) to a stable synthetic number, and rewrites the synthetic numbers the system sends back (path, query, JSON body) to the test numbers Tushe knows. `internal/numbers` documents the construction: a keyed, stateless bijection (an 8-round Feistel network over ten digits, cycle-walked off the 9999 block), so no table is kept, the same Tushe number always gives the same synthetic number under one key, and no synthetic number starts 9999. Only runs of exactly ten digits are touched: an amount written `999904123400` is never rewritten. About 1 in 10,000 ordinary ten-digit numbers is also a synthetic number; that is inherent to a stateless scheme. Before a response leaves, a scan refuses it (502, counted in `responses_refused_for_a_leaked_test_number`, which must stay 0) if any test number survived. `POST /control/v1/numbers/map` tells the runner the synthetic number of each test number.

## Tests

`make env-proxy-test` (or `cd benchmark/hostility_proxy && go test -race ./...`). The upstream in the tests is a local server standing in for Tushe's data; every fault is the proxy's own, applied on a real connection and observed by a real client.

| What | Where |
|---|---|
| each fault family, observed as specified | `internal/proxy/fault_test.go` |
| no `9999` number reaches the system under test, rewrites stable across proxies and runs, round trip over 300 numbers | `internal/proxy/numbers_test.go`, `internal/numbers` |
| reproduction from plan and seed | `TestReplayFromTheSameSeedIsIdentical`, `TestFractionalProbabilityIsSeededAndReproducible` |
| control API refuses every other identity; plans validated; profile guard | `internal/proxy/control_test.go`, `internal/config` |
| the schema's fixtures and the loader agree | `internal/plan` (`TestContractFixtures*`) |

## Run

`make env-proxy-build` builds `noema-hostility-proxy:dev`. Compose: `deploy/compose/services/hostility-proxy.yml`, in the `ci`, `bench` and `netcheck` profiles. A one-shot step writes the number key and the bench token into a named volume; nothing is generated into the repository. The proxy's one route out is the network `tushe_egress` (as `provider_egress` is the model's); that network is not in the PERD §65.1 table, which needs a docs note.

## Not done here

The sample route table is not Tushe's real interface (Tushe's OpenAPI is not frozen). Live runs against the hosted sandbox belong to G0 (ENV-05, ENV-06). The reference partner listener and the hostile proxy for the relay host are the webhook relay's item.
