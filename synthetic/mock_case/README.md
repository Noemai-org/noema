# synthetic/mock_case

**Implements:** REQ-ENV-008 (task ENV-08), REQ-ENV-004, REQ-ENV-002, REQ-ENV-006; serves the case, complaint and evidence rows of PERD §34.4. D7 (DEC-2026-10-20): the linearizability check.
**Built by:** the test-environment session (REQ-ENV-008: not the builder of `services/core`). **Fault plans:** owned by the benchmark owner.
**Status:** built. SYNTHETIC: every response carries `X-Mock-Case-Synthetic: true`, every clock is labelled `SYNTHETIC`, and every queue and priority below is a synthetic value.

mock-case emulates the bank's case system, because Tushe has no case domain (DEC-2026-10-12 D1). It is a test-environment component, not a Noema deployable (DEC-2026-10-16 E5). It is a separate Go module with its own Dockerfile and build context, its own PostgreSQL database and its own credentials. It imports nothing from `services/`, and holds no decision, grant, Record or provider credential.

## What it offers, and what it cannot

| §34.4 row | Route | Notes |
|---|---|---|
| Create with idempotency key | `POST /v1/cases` + `Idempotency-Key` | same key and same payload: `200` and the original case (`Idempotent-Replay: true`); same key and a different payload: `409 IDEMPOTENCY_CONFLICT`, nothing changes, nothing leaks |
| Find by transaction or principal; by idempotency key | `GET /v1/cases?tenant_id=&idempotency_key=` or `&principal_id=[&transaction_ref=]` | never a global search; absence is `200` with `items: []`; pages carry `complete` and `next_cursor` |
| A version token on every read | `GET /v1/cases/{id}`, and every item of a list | opaque `v<n>`: compare it, do not parse it |
| Route with expected version | `POST /v1/cases/{id}/route` | a stale token is `409 VERSION_MISMATCH` (distinct from the idempotency conflict), applies nothing, and names the current token in `details` |
| Complaints | `POST /v1/cases/{id}/complaints`, `GET /v1/complaints[/{ref}]` | same idempotency and read-back rules; `clock_started_at` is a SYNTHETIC clock that nothing can change |
| Evidence | `POST /v1/cases/{id}/evidence`, `GET /v1/evidence[/{id}]` | append-only; `payload_hash` is the SHA-256 of the exact bytes of `payload` (`sha256:<hex>`); a supplied `payload_hash` that does not match is `422` |

**Absent by construction.** There is no operation to close or resolve a case, downgrade priority, change an SLA or clock, edit or delete evidence, or move money, and no dispute. They have no route (the answer is a JSON `404`, for any method), no handler, and no entry in the API description (`internal/api/openapi.json`). The database refuses them too (`internal/store/migrations/0001_init.sql`): `status` is constrained to `OPEN`; a case row may change only its queue and its version (+1); evidence, complaints and the command log refuse UPDATE, DELETE and TRUNCATE, for the owner as well. Tests: `TestForbiddenOperationsHaveNoRouteAndChangeNothing`, `TestCaseApiDescriptionOffersNothingProhibited`, `TestDatabaseRefusesWhatTheServiceDoesNotOffer`.

## Two listeners, two credentials

| Listener | Network (PERD §65.1) | Opens for | API |
|---|---|---|---|
| case, `:8090` | `tushe_net` | the **runtime** credential (the core's case adapter); `/healthz` needs none | `/v1/...` |
| bench, `:8091` | `bench_net` **only** | the **bench** credential (the benchmark runner) | `/truth/v1/...`, `/control/v1/...` |

The bench listener is bound to the one local address that shares a subnet with `MOCK_CASE_BENCH_BIND_VIA` (the benchmark database, which is on `bench_net` only), so a caller on `tushe_net` cannot connect at all. If that address cannot be found, mock-case does not start. On top of the network, the credential is checked: every runtime identity (the adapter's credential, no credential, a guessed one) is refused on the bench listener, with `403` for a known runtime credential, and every refusal is logged (REQ-ENV-002). The connection-test matrix (`deploy/compose/netcheck/matrix.py`, step 6) proves both layers. The two tokens must differ and are read from files, never from the environment.

The truth endpoint: `GET /truth/v1/state` (all cases, complaints, attachments), `/truth/v1/commands` (the command log: every command and every injected fault, with scenario, seed and fault-plan ID), `/truth/v1/digest` (a state hash and a fault-trace hash, to compare two replays in one call). Control: `PUT|GET|DELETE /control/v1/fault-plan`, `GET /control/v1/clock`, `POST /control/v1/clock/advance`, `POST /control/v1/reset`. There is no snapshot or restore (PERD §34.4).

## Fault plans

mock-case applies a FaultPlan (PERD §37.1, JSON encoding) loaded through `PUT /control/v1/fault-plan`. It accepts only a plan it can apply exactly, and refuses the rest when loading (`422 INVALID_FAULT_PLAN`), so a typo cannot become a plan that injects nothing:

- `source` is `MOCK_CASE`, `deterministic` is `true`, every fault's `target` is `mock-case`, unknown fields are refused.
- `operation`: `create_case`, `find_cases`, `get_case`, `route_case`, `create_complaint`, `find_complaints`, `get_complaint`, `append_evidence`, `find_evidence`, `get_evidence`.
- `match`: `nth_call` (1 or more) and exact-match request attributes that the operation can actually supply (`tenant_id`, `principal_id`, `transaction_ref`, `idempotency_key`, `case_id`). A fault's counter counts only the calls that match its other keys, so unrelated traffic never moves `nth_call`. The first matching fault in plan order applies. A malformed request is counted but never faulted.
- `effect` and `params`:

| Effect | Params | What happens |
|---|---|---|
| `DROP_RESPONSE` | none | the command commits, then the connection is dropped (commit-then-drop) |
| `TIMEOUT_AFTER_COMMIT` | `delay_ms` | the command commits, the response is held `delay_ms`, then the connection is dropped |
| `TIMEOUT_BEFORE_COMMIT` | `delay_ms` | nothing is committed; held `delay_ms`, then dropped |
| `HTTP_5XX` | `status` (500 to 599, default 500), `after_commit` (default false) | a 5xx; with `after_commit` the command commits first |
| `HTTP_429` | `retry_after_seconds` (optional) | `429`, with `Retry-After` only if given; nothing is committed |
| `LATENCY` | `delay_ms` | the call is delayed, then runs normally |
| `PAGINATE` | `page_size` | a list returns at most `page_size` items, with `complete: false` and a cursor (list operations only) |
| `CONCURRENT_MODIFICATION` | none | on `route_case`: a competing modification advances the version, so the route is refused as stale (the stale-version rejection) |

A fault never marks the response: no header or field tells the caller a fault was injected (`TestFaultsAreBlindToTheCaller`). Faults are visible only on the bench side, in the command log.

- `clock.start` starts a simulated clock (all stored times follow it, in UTC); `clock.advances[]` are applied when the runner calls `POST /control/v1/clock/advance {"step": "<at_step>"}`; `{"by": "PT31S"}` advances directly. Without a `clock` block the wall clock is used.
- `seed` makes identifiers and references a pure function of (seed, kind, counter, clock): the same plan, seed and simulated clock give identical IDs, state and fault trace (`TestReplayFromTheSameSeedIsIdentical`, via `/truth/v1/digest`).

The operation names, params and match keys above are mock-case's reading of PERD §37.1, which names only `create_case`, `TIMEOUT_AFTER_COMMIT`, `nth_call` and `delay_ms`. The fault-plan schema (`contracts/schemas/fault-plan.v1.json`, owned by the benchmark owner) should adopt them or replace them; `internal/faultplan` is the one place to change.

## Tests

`scripts/test.sh` runs everything against a real PostgreSQL: it uses `MOCK_CASE_TEST_DATABASE_URL` if set (CI points it at a service container), otherwise it starts and removes a throwaway server (it uses `runuser` when run as root). A missing variable fails a test; nothing is skipped. `make env-mock-case-test` runs it with `-race` and the format and vet checks.

| What | Where |
|---|---|
| Contract tests for every §34.4 row it serves | `internal/api/contract_test.go` |
| Commit-then-drop and every other fault, on the wire, reproducible from a seed | `internal/api/faults_test.go` |
| No route, no description entry, no database path for the prohibited operations | `internal/api/absent_test.go`, `internal/store/store_test.go` |
| The truth endpoint refuses every runtime identity | `internal/api/auth_test.go`; in Compose: matrix step 6 |
| Linearizability (D7): concurrent histories under faults, checked per key; a corrupted history is rejected | `internal/api/linearizability_test.go`, `internal/lincheck` |
| Plan validation, counters, determinism | `internal/faultplan` |

The checker (`internal/lincheck`) is a Wing and Gong search with memoisation, over the standard library only. An operation whose response was never seen (a drop, a timeout, a 5xx) is *pending*: it may have taken effect at any time after it was invoked, or never. A budget overrun is a failure, not a pass.

## Run

`make env-mock-case-build` builds `noema-mock-case:dev`. Compose: `deploy/compose/services/mock-case.yml` (included by the main file). It is in the `ci`, `bench`, `demo` and `netcheck` profiles, not yet in `dev`, because the core's case adapter does not exist; add `dev` then. A one-shot `mock-case-credentials` step writes random credentials into named volumes (nothing is generated into the repository): the case adapter will mount only `mock_case_runtime_credential`, the benchmark runner only `mock_case_bench_credential`.

Configuration is in `internal/config` (`MOCK_CASE_*`); secrets are `*_FILE` paths only. Default queues (`PAYMENTS_OPERATIONS`, `DISPUTES`, `FRAUD_REVIEW`, `SETTLEMENT_OPS`) mirror the pack's routing allowlist and priorities (`NORMAL`, `HIGH`) are placeholders: both are SYNTHETIC configuration, not a definition of any bank's system.

## Not done here

The core's case client (`services/core/adapters/synthetic_tushe/`) is session A's. The fault-plan schema, the hostility proxy and the webhook relay are separate items of this session's list.
