# synthetic/mock_case

**Implements:** REQ-ENV-008 (task ENV-08), REQ-ENV-004, REQ-ENV-002, REQ-ENV-006; serves the case, complaint and evidence rows of PERD §34.4. D7 (DEC-2026-10-20): the linearizability check.
**Built by:** the test-environment session (REQ-ENV-008: not the builder of `services/core`). **Fault plans:** owned by the benchmark owner.
**Status:** built. SYNTHETIC: every response carries `X-Mock-Case-Synthetic: true`, every clock is labelled `SYNTHETIC`, and every queue and priority below is a synthetic value.

mock-case emulates the bank's case system, because Tushe has no case domain (DEC-2026-10-12 D1). It is a test-environment component, not a Noema deployable (DEC-2026-10-16 E5). It is a separate Go module with its own Dockerfile and build context, its own PostgreSQL database and its own credentials. It imports nothing from `services/`, and holds no decision, grant, Record or provider credential.

## What it offers, and what it cannot

| §34.4 row | Route | Notes |
|---|---|---|
| Create if absent, with idempotency key | `POST /v1/cases` + `Idempotency-Key` + `If-None-Match: *` | the create is conditional on there being no case for the tenant, transaction and epoch. Same key and same payload: `200` and the original case (`Idempotent-Replay: true`); same key and a different payload: `409 IDEMPOTENCY_CONFLICT`, nothing changes, nothing leaks; another key for a subject that has a case: `412 PRECONDITION_FAILED`, nothing is written and no reference is spent, and `details` names `existing_case_id` and `existing_version_token`; no `If-None-Match: *`: `428 PRECONDITION_REQUIRED` (an unconditional create has no route) |
| Find by transaction or principal; by idempotency key | `GET /v1/cases?tenant_id=&idempotency_key=` or `&principal_id=[&transaction_ref=]` | never a global search; absence is `200` with `items: []`; pages carry `complete` and `next_cursor` |
| A version token on every read | `GET /v1/cases/{id}`, and every item of a list | opaque `v<n>`: compare it, do not parse it |
| Route with expected version | `POST /v1/cases/{id}/route` | a stale token is `409 VERSION_MISMATCH` (distinct from the idempotency conflict), applies nothing, and names the current token in `details` |
| Complaints | `POST /v1/cases/{id}/complaints`, `GET /v1/complaints[/{ref}]` | same idempotency and read-back rules; `clock_started_at` is a SYNTHETIC clock that nothing can change |
| Evidence | `POST /v1/cases/{id}/evidence`, `GET /v1/evidence[/{id}]` | append-only; `payload_hash` is the SHA-256 of the exact bytes of `payload` (`sha256:<hex>`); a supplied `payload_hash` that does not match is `422` |

**Absent by construction.** There is no operation to close or resolve a case, downgrade priority, change an SLA or clock, edit or delete evidence, or move money, and no dispute. They have no route (the answer is a JSON `404`, for any method), no handler, and no entry in the API description (`internal/api/openapi.json`). The database refuses them too (`internal/store/migrations/0001_init.sql`): `status` is `OPEN` or `ROUTED` and can only move from OPEN to ROUTED (never back, never closed); a case row may change only its queue and its version (+1); evidence, complaints and the command log refuse UPDATE, DELETE and TRUNCATE, for the owner as well. Tests: `TestForbiddenOperationsHaveNoRouteAndChangeNothing`, `TestCaseApiDescriptionOffersNothingProhibited`, `TestDatabaseRefusesWhatTheServiceDoesNotOffer`.

## Create if absent

`create_escalation_case` stays `CONDITIONAL_WRITE` in the capability descriptor (nothing in the SoT or the descriptor changes). This is how mock-case honours the condition, so the core can rely on it when the dispatch outcome is unknown (PERD §27.1 E7, REQ-EXE-004).

- **The precondition is the request's.** `If-None-Match: *` says "create only if absent". Absent means: no case in `OPEN` or `ROUTED` for the same `tenant_id`, `transaction_ref` and `epoch`.
- **It is enforced atomically in the target.** The unique partial index `cases_subject_epoch_uq` (migration `0003_create_if_absent.sql`) is the guard, in the transaction that would insert, so two concurrent creators cannot both succeed. A creator that loses is rolled back to before it took a reference number, so references stay gapless.
- **The idempotency key is the retry's identity.** The core derives it from the plan inputs bound to the grant, so a retry under a new grant (PERD §27.2) sends the same key. Same key, same payload is the original case; same key, different payload is `409`. A different key for the same subject is a different attempt at the same create, and is `412`.
- **The refusal can be verified.** The `412` names the case that exists and its server-issued version token. The caller reads it back (`GET /v1/cases/{id}`) and compares; no token is made up (a made-up token on a route is `409 VERSION_MISMATCH`).
- **Scope of the guarantee.** It holds for mock-case only. How Tushe's own create behaves is an open item for the founders; the core must not read this section as a statement about Tushe.

Tests: `internal/api/precondition_test.go` (header required, duplicate, conflicting, concurrent under load, read-back), `internal/store/store_test.go` (`TestCreateCaseIfAbsentRefusesASecondCaseForTheSubject`, `TestTheDatabaseRefusesASecondOpenCaseForTheSubject`, `TestConcurrentCreatesForOneSubjectYieldOneCase`), and the linearizability workload, whose model includes a rival key per subject.

## Two listeners, two credentials

| Listener | Network (PERD §65.1) | Opens for | API |
|---|---|---|---|
| case, `:8090` | `tushe_net` | the **runtime** credential (the core's case adapter); `/healthz` needs none | `/v1/...` |
| bench, `:8091` | `bench_net` **only** | the **bench** credential (the benchmark runner) | `/truth/v1/...`, `/control/v1/...` |

The bench listener is bound to the one local address that shares a subnet with `MOCK_CASE_BENCH_BIND_VIA` (the benchmark database, which is on `bench_net` only), so a caller on `tushe_net` cannot connect at all. If that address cannot be found, mock-case does not start. On top of the network, the credential is checked: every runtime identity (the adapter's credential, no credential, a guessed one) is refused on the bench listener, with `403` for a known runtime credential, and every refusal is logged (REQ-ENV-002). The connection-test matrix (`deploy/compose/netcheck/matrix.py`, step 6) proves both layers. The two tokens must differ and are read from files, never from the environment.

The truth endpoint: `GET /truth/v1/state` (all cases, complaints, attachments), `/truth/v1/commands` (the command log: every command and every injected fault, with scenario, seed and fault-plan ID), `/truth/v1/digest` (a state hash and a fault-trace hash, to compare two replays in one call). Control: `PUT|GET|DELETE /control/v1/fault-plan`, `GET /control/v1/clock`, `POST /control/v1/clock/advance`, `POST /control/v1/reset`. There is no snapshot or restore (PERD §34.4).

## Reconciliation with the pack

The pack's `case` entity (`packs/fintech_v1.json`; SoT §15.3 success predicate `case.status in ['OPEN','ROUTED']` and `case.created_by_intent == intent_id`) reads three things the first mock-case did not supply. **mock-case was the incomplete side, so mock-case changed and the pack did not.** The SoT predicate is normative; the PERD §34.4 `fin.case` shape is `[REC]` and says exact field names are fixed at Phase A.

| Pack reads | Before | Now (migration `0002_intent_epoch_routed.sql`) |
|---|---|---|
| `created_by_intent` | not stored | required on create (`created_by_intent`, same pattern as other IDs), returned on every read, part of the idempotent payload, fixed for life |
| `epoch` | not stored | required on create (integer 0 to 2^53-1), returned on every read, part of the idempotent payload, fixed for life |
| status `ROUTED` | only `OPEN` existed; routing moved the queue | a successful route moves OPEN to ROUTED; a later route keeps ROUTED; the database refuses ROUTED back to OPEN and any other status |

Still true: nothing closes or resolves a case. The pack's status enum keeps `CLOSED`, which mock-case never emits, so the epoch can never advance against mock-case (PERD §29: it increments when the prior case is verified CLOSED). A benchmark that needs epoch 1 must create the case with `epoch: 1`. The pack's `id` and `subject` are the adapter's renames of `case_id` and `transaction_ref`. `packs/tests/test_mock_case_shape.py` pins all of this. SYNTHETIC.

## Fault plans

mock-case applies a FaultPlan loaded through `PUT /control/v1/fault-plan`. The format is `contracts/schemas/fault-plan.v1.json` (PERD §37.1, JSON); `internal/faultplan` loads it and `TestContractFixtures*` runs every valid and invalid fixture of the schema through the loader, so the two cannot drift apart.

- **Whose faults.** A plan may hold faults for several targets (the PERD §37.1 example holds a `mock-case` fault and a `mock-switch` fault). mock-case applies exactly the faults whose `target` is `mock-case` and counts the rest as ignored: `GET /control/v1/fault-plan` reports `applied_faults` and `ignored_faults`. A plan whose `source` is `MOCK_CASE` holds mock-case faults only. A target or effect no plan may name is refused, so a typo cannot become a plan that injects nothing.
- `deterministic: true` limits every `probability` to 0 or 1; mock-case has no probabilistic fault, so a mock-case fault's probability is 0 (never fires, consumes no call) or 1 (the default).
- `operation`: `create_case`, `find_cases`, `get_case`, `route_case`, `create_complaint`, `find_complaints`, `get_complaint`, `append_evidence`, `find_evidence`, `get_evidence`.
- `match`: `nth_call` (1 or more) and exact-match request attributes the operation can actually supply (`tenant_id`, `principal_id`, `transaction_ref`, `idempotency_key`, `case_id`). A fault's counter counts only the calls that match its other keys, so unrelated traffic never moves `nth_call`. The first matching fault in plan order applies. A malformed request is counted but never faulted. The fault trace names each fault by its position in the plan's `faults`.
- `effect` and `params` (the schema fixes them per effect):

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
| `NEAR_TIMEOUT_BEFORE` | `delay_ms` | the answer comes `delay_ms` **before** the commit. A caller that waits gets the real answer; a caller that gives up first leaves nothing committed (EC-T09). `delay_ms` puts the answer just inside or just outside the caller's timeout |
| `NEAR_TIMEOUT_AFTER` | `delay_ms` | the command commits at once and the real answer is held for `delay_ms`. A caller that gives up first has a committed case it never saw; one that waits gets the real answer, late (EC-T09). Unlike `TIMEOUT_AFTER_COMMIT`, the answer is not dropped |
| `HTTP_200_SEMANTIC_PENDING` | none | on the keyed writes (`create_case`, `create_complaint`, `append_evidence`): HTTP 200 with `{"status":"PENDING","detail":...}`, no created object, **nothing created**; the key stays free (EC-E03) |
| `IDEMPOTENCY_COLLISION` | none | on the keyed writes: answered `409 IDEMPOTENCY_CONFLICT`, word for word as a real collision, with nothing created (EC-E06) |
| `INJECTION_PAYLOAD` | `field` | on the reads: the SYNTHETIC text `InjectedText` ("ignore all previous instructions...") is appended to one field of the answer, `reason_code` (`get_case`, `find_cases`) or `payload` (`get_evidence`, `find_evidence`). The stored data and the truth endpoint are unchanged (EC-X02, AGENT-09) |

A fault never marks the response: no header or field tells the caller a fault was injected (`TestFaultsAreBlindToTheCaller`). Faults are visible only on the bench side, in the command log.

- `clock.start` starts a simulated clock (all stored times follow it, in UTC); `clock.advances[]` are applied when the runner calls `POST /control/v1/clock/advance {"step": "<at_step>"}`; `{"by": "PT31S"}` advances directly. Without a `clock` block the wall clock is used.
- `seed` makes identifiers and references a pure function of (seed, kind, counter, clock): the same plan, seed and simulated clock give identical IDs, state and fault trace (`TestReplayFromTheSameSeedIsIdentical`, via `/truth/v1/digest`).

## Tests

`scripts/test.sh` runs everything against a real PostgreSQL: it uses `MOCK_CASE_TEST_DATABASE_URL` if set (CI points it at a service container), otherwise it starts and removes a throwaway server (it uses `runuser` when run as root). A missing variable fails a test; nothing is skipped. `make env-mock-case-test` runs it with `-race` and the format and vet checks.

| What | Where |
|---|---|
| Contract tests for every §34.4 row it serves | `internal/api/contract_test.go` |
| Commit-then-drop and every other fault, on the wire, reproducible from a seed | `internal/api/faults_test.go` |
| No route, no description entry, no database path for the prohibited operations | `internal/api/absent_test.go`, `internal/store/store_test.go` |
| The truth endpoint refuses every runtime identity | `internal/api/auth_test.go`; in Compose: matrix step 6 |
| Linearizability (D7): concurrent histories under faults, checked per key; a corrupted history is rejected. Clients wait 300 ms when faults are loaded (a 600 ms hold is a lost response) and 10 s in the quiet run, where nothing may be lost | `internal/api/linearizability_test.go`, `internal/lincheck` |
| Plan validation, counters, determinism | `internal/faultplan` |

The checker (`internal/lincheck`) is a Wing and Gong search with memoisation, over the standard library only. An operation whose response was never seen (a drop, a timeout, a 5xx) is *pending*: it may have taken effect at any time after it was invoked, or never. A budget overrun is a failure, not a pass.

## Run

`make env-mock-case-build` builds `noema-mock-case:dev`. Compose: `deploy/compose/services/mock-case.yml` (included by the main file). It is in the `ci`, `bench`, `demo` and `netcheck` profiles, not yet in `dev`, because the core's case adapter does not exist; add `dev` then. A one-shot `mock-case-credentials` step writes random credentials into named volumes (nothing is generated into the repository): the case adapter will mount only `mock_case_runtime_credential`, the benchmark runner only `mock_case_bench_credential`.

Configuration is in `internal/config` (`MOCK_CASE_*`); secrets are `*_FILE` paths only. Default queues (`PAYMENTS_OPERATIONS`, `DISPUTES`, `FRAUD_REVIEW`, `SETTLEMENT_OPS`) mirror the pack's routing allowlist and priorities (`NORMAL`, `HIGH`) are placeholders: both are SYNTHETIC configuration, not a definition of any bank's system.

## Not done here

The core's case client (`services/core/adapters/synthetic_tushe/`) is session A's. The fault-plan schema, the hostility proxy and the webhook relay are separate items of this session's list.
