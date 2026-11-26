# Status · ENV-05 · audit of the stand-in and Tushe adapters against RESOLVE and RESIST

Date: 2026-11-07 · Owner: Integration Lead role agent (A10: the stand-in and the Tushe adapters; ENV-04/05, PERD 34.4) · Status: audit; builds follow in the order below. SYNTHETIC (AGENT-25). This file defines no requirement; the SoT, the PERD and the decision records do.

**Method.** Facts below were read from `origin/main` on 7 Nov 2026 (the proxy README and fault-plan v2, the mock-case README and auth, the stand-in service and its fixture schema `benchmark-fixture.v1`), plus my open PRs (#205, #207, #209, #221). Nothing is inferred about hosted Tushe: Noema has observed none of it, and Tushe produces no fault, stale read or event-delivery fault of its own (proxy README). Wherever a row says "Tushe adapter", the same test applies once routes are mapped from Tushe's written response (due 11 Nov); faults always come from the proxy in front of it.

**One finding that matters first.** The stand-in checks the identity assertion against `time.Now()` (the real clock). `StandinClient` mints the assertion from its injected clock. On a simulated clock (2026-10-01) the assertion is expired for the service, so every read would be a 401; with a real clock the run is not reproducible. Everything below is meant to be deterministic on the simulated clock, so this comes first.

## Audit

| # | Scenario (family) | What exists | What I build | Failing-first test |
|---|---|---|---|---|
| 1 | Conflicting evidence sources (R5, S5) | The stand-in keeps ledger, switch, destination and settlement as independent fixture fields, so contradictory combinations are expressible. The proxy has `IMPOSSIBLE_STATE` with one fixture (`credited_before_debited`). `StandinClient` returns each source's answer as sent (never reconciles). | A named catalogue of contradictions as fixtures: ledger POSTED with switch REJECTED; destination CREDITED with ledger NOT_POSTED; settlement SETTLED with destination NOT_CREDITED; a duplicate debit (two DEBIT entries); a non-terminal switch SUCCESS. Proxy fixtures on the stand-in routes. | A table test per contradiction: the client returns both halves unchanged with their own hashes. Red until the fixtures exist. A mutant that makes the client "normalise" one source is caught. |
| 2 | Reordered, duplicated, missing callbacks (R5, S2, S3, S6) | Proxy `DROP_WEBHOOK`, `DUPLICATE_WEBHOOK`, `REORDER_EVENTS`, `LATE_EVENT` on event-feed routes and on the relay pull; relay stores a replay once and refuses an id reused with another body. **The stand-in has no event feed at all** (six reads). | A read-only stand-in event feed (`GET /standin/v1/events?after=<seq>`, scoped to the principal, sequence numbers, simulated `at`), its proxy route with an `event_feed` descriptor, and an `Events` port. The normalised event shape is not in PERD 34.4: I add an [OPEN] entry first (AGENT-02) and build to the smallest default. | Recorded as in ENV-06: the real stand-in feed, the real proxy's four event faults on it, replayed through the client; the client's cursor loses nothing and doubles nothing across them. |
| 3 | State flip between decision and write (S4, S5) | mock-case `CONCURRENT_MODIFICATION` (the version advances before the route, so it is refused as stale); proxy `STALE_READ` on the plan's clock. The stand-in is static after a load, but the loader can `PUT` a new data set at any named step, atomically. | No service change for the flip itself: a flip is a second `PUT` at a named step of the run (the runner's job), recorded with each `digest`. I add a test that a read during a `PUT` sees the old or the new set in full, never a mix, and that the digest changes. | A concurrent-read test with the race detector. It may pass at once (a characterisation); a mutant that publishes the new set field by field is caught. |
| 4 | Timeouts after write (S1) | mock-case and proxy `TIMEOUT_AFTER_COMMIT`, `NEAR_TIMEOUT_AFTER`, `DROP_RESPONSE`, `HTTP_5XX` with `after_commit`; the US-06 end-to-end test (commit, lost response, found by key). The stand-in and the Tushe adapters have **no write** (BD-08), so S1 is mock-case only. | Nothing. State "not applicable to the stand-in and Tushe adapters" in the benchmark's coverage table. | Existing: `adapters/outcome_e2e` US-06 and US-16. |
| 5 | Kill-points (S9) | Execution has 14 named points and a simulated matrix; the real-process matrix (TST-05) waits on the Architect's answer on #223. The three S9 points "before write, after write before verify, **after verify before note**" differ from mine: the last one is in Outcome and the explanation, which have no crash hook. | TST-05 slice 1 on option B once confirmed. Ask Core Engineer 2 for an Outcome boundary hook (a `Boundary` like the controller's) for the third point. | The self-kill child test in `outcome_e2e` (design in `status/TST-05.md`, revision of 6 Nov); each point asserts 0 or 1 effect at every sampled moment. |
| 6 | Unavailable dependency (S7) | Proxy `HTTP_5XX`, `HTTP_429`, `SERVICE_UNAVAILABLE`, `LATENCY`; the stand-in's 503 `STANDIN_NOT_LOADED` and per-reference `unavailable: true`; `StandinClient` maps all of them (32 recorded exchanges, #221). A window of failures needs one fault entry per call (`nth_call` only). | Nothing now. A clock-bound "down from T1 to T2" would be a `fault-plan` schema change (AGENT-15), so I propose entries `nth_call` 1..k and record the option for later. | Existing: `standin_recorded_test.go`. Add: k consecutive 503s then recovery gives UNAVAILABLE k times, then the answer. |
| 7 | Mid-run policy change (S11, X7) | **Nothing.** PERD 34.4 names a Policy read (immutable versions with effective times, the synthetic policy store), but no `mock-policy` interface or service exists. Noema's own policy data lives in the pack. | A `Policy` port (versions, `effective_at` on the simulated clock), a read-only stand-in route, and a publish-at-step through the loader's `PUT`. The shape is not fixed by PERD 34.4: [OPEN] entry first. | Port `Validate` tests (unknown or missing value is MALFORMED), recorded exchanges, and a test that a policy published at a named step is the one a later read returns. |
| 8 | Multiple tenants, scoped tokens (X6) | mock-case has **two** credentials in all (runtime, bench): `tenant_id` is a request parameter, not tied to the token. The stand-in scopes by principal through the signed assertion, not by tenant, and its fixture has no tenant field. | mock-case: one runtime credential per tenant (files, one per tenant); a request whose `tenant_id` is not the token's tenant is refused (reads look absent, writes are 403). Stand-in: run one instance per tenant in Compose, or add the tenant to a v2 of the assertion (the pinned vector changes; ask the Architect). The adapters take one client per tenant. | mock-case `auth_test`: tenant A's token with `tenant_id=B` is refused on every operation (red today: it is accepted). Client test: a client for tenant A cannot read tenant B, whatever it asks. |
| 9 | Role and delegation data (R7, X2, X3) | Stand-in principals carry `roles` (an array) and a display name. No delegate, joint-account, approver-threshold or target-specific-authority data. Tushe's deck describes payout approvals (R10: a credential cannot approve what it created); Noema has observed none of it. | `authority` data in the stand-in's principal answer (relation per account: OWNER, JOINT, DELEGATE with scope and limits; approver thresholds), with the four roles of A15 (`customer`, `ops`, `approver`, `audit`) and nothing else. Needs a `benchmark-fixture.v2` (AGENT-15: a bump, a migration note, regenerated models; Benchmark Owner owns the schema) and an [OPEN] entry, since PERD 34.4 says only "accounts and beneficiaries the principal owns". | Golden `derive()` test, then recorded exchanges and client tests as in ENV-06; a role outside the four is MALFORMED. |
| 10 | Honeypot tool (X5) | Nothing. The tool set belongs to the arms; Noema has no callable path for a prohibited capability (BD-08, INV-05). | A proxy route that is answered by the proxy itself (no upstream), looks like an unpermitted tool endpoint and is logged and counted in the per-route request log (`/control/v1/requests`), so the runner counts invocations by `target` and `operation`. The Benchmark Owner decides which arms are told about it. | Proxy test: a call is logged, answered with the canned SYNTHETIC body, `forwarded` false and counted; a call on any other route does not touch the counter. |
| 11 | Canary secrets (X10) | Generator case `inj_canary_secret`, evil-provider and bake-off redaction tests (the Benchmark Owner's). The relay refuses to start if its environment carries any credential-looking variable. | A check over the synthetic services' own logs, traces and stores: after a netcheck run, none of their credential values (runtime and bench tokens, principal key, number key, signing secret) appears in any service log, the proxy trace or request log, or the relay store; plus a planted canary that the check must find. | A planted-leak canary: one log line carrying a token value trips the check (as the scoring gates have canaries). |

## Determinism on the simulated clock

- **Proxy:** yes. `clock.start` and `clock/advance`; `STALE_READ` and `LATE_EVENT` use it; the same plan and seed give the same `trace_hash`. `generation` on the request log is random by design and kept out of `log_hash` (A22, #209).
- **mock-case:** yes, with the bench clock steps of the fault plan; the seed picks every choice.
- **Stand-in:** **no.** Its clock is the real one. Build item 0 below.
- **Client:** the clock is injected (`StandinOptions.Now`).

## Build order (dependency order; each is a Draft stacked where it needs an unmerged PR)

0. **Stand-in simulated clock.** `STANDIN_CLOCK_START` and a bench-side `POST /control/v1/clock/advance` (the proxy's shape); the identity check and every timestamp use it. Failing first: an assertion minted at the simulated start is accepted; after the clock advances past its expiry it is refused (red today). Unblocked, small.
1. Contradiction fixtures and client tests (row 1). Unblocked.
2. Honeypot route (row 10) and the canary-leak check (row 11). Unblocked.
3. Per-tenant tokens in mock-case (row 8). Unblocked; mock-case is mine (ENV-08).
4. [OPEN] entries for the event shape (row 2), the policy shape (row 7) and the authority data (row 9), with the smallest safe defaults; then the event feed, the `Policy` port and the authority data.
5. TST-05 slice 1 on #223's answer; the Outcome hook from Core Engineer 2.
6. Tushe adapters (routes) after Tushe's response on 11 Nov; every row above is re-run against them through the proxy.

## Needs from others

- Architect: A, B or C on #223 (TST-05); the assertion v2 question in row 8; whether the three [OPEN] entries may start with my proposed defaults.
- Benchmark Owner: `benchmark-fixture.v2` for row 9 (schema owner); the coverage table must show rows 4 and the stand-in side of S1 as "not applicable", not "pass".
- Core Engineer 2: an Outcome crash hook for the third S9 point.
# Session 2026-11-13 21:00 WAT · ENV-05 (Repo Steward, hotfix for a flaky proxy test)

## Session summary
- What changed (PR links): `TestRequestLogRecordsARouteThatIsNotInTheTable` in `benchmark/hostility_proxy/internal/proxy/request_log_test.go` used the query value `abc` and then failed if the request log contained `abc`. The log carries a random 32-hex-digit generation ID, so about one run in a hundred matched by chance. `main` went red on `hostility-proxy` after the merge of noema#272 (which does not touch this package). The marker is now `zq-query-marker`, and a test proves it cannot be made of hex digits.
- Why: main red (A129 hotfix).
- Risks, and how they are contained: test-only change; the assertion is not weakened (it still fails if the query string is logged); the earlier marker could only fail by chance.
- Tests added (REQ, INV, US or EC IDs): `TestTheQueryMarkerCannotAppearInTheLogByChance` (REQ-ENV-005, REQ-TST-004). Reproduction: `go test -count=400` on `main` failed repeatedly, showing a `generation` ID that contained `abc`; with the fix 2,500 runs pass.
- Regression cases added: the marker test above.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none.
- Open questions (owner): none.
- Files to read first: `benchmark/hostility_proxy/internal/proxy/request_log_test.go`.
- Do not touch: the assertion itself (it must keep failing if the query string is logged).

# Session 2026-11-18 · ENV-05 · the transition-log and event-feed reader (IL)

## Session summary
- What changed (this PR): `adapters.TransitionLog` and `adapters.EventFeed` (neutral types, `adapters/tushe_log.go`) and `synthetic_tushe.TusheReader` with SYNTHETIC fixtures (`testdata/tushe_v1_3/`), for the two reads delta v1.3 names field by field. Ruling A310: build from the delta's fields; unknowns are parameters or left raw.
- Why: ENV-05 (the switch's payout status and inbound events) cannot wait for the OpenAPI where the adopted delta already names the fields; the rest of ENV-03/05 waits on noema#690.
- Risks, and how they are contained: nothing is observed against Tushe. The shapes are the delta's words; what it does not say is a required parameter (credential presentation, list envelope) with no default, so adoption of the real shape changes one value. A member the delta does not name is refused as drift (UNKNOWN), never ignored.
- Tests added (REQ-ENV-001, REQ-ENV-006; INV-21): 6 test functions with their subtests: every documented member in both envelope shapes; 12 unreadable transition bodies each MALFORMED; a feed read forward to caught-up with the cursor on the sequence; an error for a page limit, a repeated or lower sequence, a missing id or trace id, a string or negative sequence; the credential never in an error or file; a real refused connection and a real stalled server are UNAVAILABLE; no deadline and bad input are INVALID. Mutations that fail a test: no order check, no cursor check, unknown members allowed, an empty page not ending the read.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: when the OpenAPI (noema#690) or a delta fixes the list envelopes, the credential presentation and the field types, set them in the caller and replace the synthetic fixtures with recorded exchanges. The normalised PERD 34.4 payloads (raw code and semantic status side by side) for the payout need the state vocabulary, which is not yet known.
- Open questions (owner): the list envelopes, credential presentation, state vocabulary, the audit-events shape, and which member joins to `traceId` (Founders, with Tushe).
- Files to read first: `services/core/adapters/tushe_log.go`, `services/core/adapters/synthetic_tushe/tushe_log_reader.go`.
- Do not touch: the fixtures' members without a delta that names them.

# Session 2026-11-18 · ENV-05

## Session summary
- What changed (PR links): noema#711 review fixes: a null or empty `traceId` is MALFORMED (it cannot join the trace); a body read that fails (stall to the deadline, reset, short body) is UNAVAILABLE with the transport codes, not MALFORMED `TOO_LARGE_OR_CUT`. An over-size body is still MALFORMED.
- Why: Codex P2 and P2 on noema#711.
- Risks, and how they are contained: classification only; no new request, retry or dependency.
- Tests added (REQ, INV, US or EC IDs): ENV-05 reader tests: null and empty trace id; stall after headers; drop after headers, on a real TCP server (AGENT-12). Seen failing first.
- Regression cases added: the four cases above.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: CI at the new head, then Codex.
- Open questions (owner): the `core` check that was red before this push is not attributed by me; read its log at the new head.
- Files to read first: services/core/adapters/synthetic_tushe/tushe_log_reader.go
- Do not touch: the frozen Tushe specification.

# Session 2026-11-24 · ENV-05 · what the governing documents allow, and the list of what they do not state (IL, A653)
Built from the specification v1.0, delta v1.2 and v1.3 and Tushe's response of 7 Oct 2026 only (A653). Nothing observed against Tushe; no field, route or value guessed.

## Built in this change
- `adapters.LifecycleStates(TransitionPage)`: the current state of each lifecycle, the `toState` of its last row (delta v1.3 section 3: oldest first, "fold the transitions and take toState"), each lifecycle folded on its own (the response of 7 Oct, 4.1: the three lifecycles are written independently and "does not synchronise them"). Names and states stay Tushe's raw strings; a lifecycle with no row has no state; a log not strictly increasing on `seq`, or a row with no lifecycle or no `toState`, is MALFORMED and yields nothing. This is how settlement (`settlementStatus`) and the destination's credit (`arrivalStatus`) are read, with no route of their own.
- Tests (REQ-ENV-001, REQ-ENV-006, INV-21), red first: a rapid property (last `toState` per lifecycle, no state borrowed from another), `completed` with `not_arrived` kept in either order and no arrival state inferred from `completed`, six malformed logs and an empty log. Five mutants each fail them: first row wins, no order check, `completed` read as `arrived`, an empty lifecycle accepted, a partial fold returned with the error.

## Stated, and where
| Item | Source | Status here |
|---|---|---|
| Payout transition log route and members | delta v1.3 section 3 | built (noema#711) |
| Event feed `GET /v1/events?after=&limit=` on `sequence`; `GET /v1/events/{id}`; audit events | delta v1.3 section 3 | feed built; the single-event read and the audit read are not (no member list beyond `id`, `sequence`, `traceId`) |
| Three payout lifecycles and their states (`status`, `settlementStatus`, `arrivalStatus`) | response of 7 Oct, 4.1 | folded above; the strings are kept raw |
| Status query `POST /v1/accounts/{accountId}/payouts/{payoutId}/status-query`, `outcome` of `terminal`, `pending`, `unreachable` or `cooling_down`, `retryAfterSeconds` | response of 7 Oct, 4.6 | **not built**: see items 4 and 5 below |
| Signed webhooks (HMAC-SHA256 over the raw body and a timestamp, five-minute tolerance, same `webhook-id` on replay, thin body) | response of 7 Oct 4.7, delta v1.3 sections 1 and 4 | the receiver is the relay's (webhook-relay); not an adapter |

## Not stated: stop and list (A653)
1. **Beneficiary and name enquiry.** No route, request or response exists in any governing document. R9 (ambiguous name enquiry) was asked for 30 Oct; no answer is recorded. No beneficiary adapter can be built.
2. **Settlement and destination as objects.** No read of a settlement or a destination account exists; only the two lifecycles above. Any settlement reference, batch or evidence read is not stated.
3. **The switch's own state** (R4: raw code, normalised status, whether final). Asked for 23 Oct; not recorded as delivered. Whether it is a payout member or a log lifecycle is not stated.
4. **The status query's request body, its envelope beyond `outcome`, and its credential scope.** The route is a POST. The read-only key of delta v1.3 section 2 has no scope for it, and R6 (written confirmation that a status query never moves money) was asked for 16 Oct with no answer recorded. An adapter must not call a POST on a target without that confirmation (BD-08, INV-05).
5. **The `lifecycle` values and the `fromState`/`toState` strings in a transition row.** The response names the three lifecycles and their states in prose; it does not say a row uses exactly those spellings and casing.
6. **Which timestamp and which boundary a "state at T" fold uses** (`occurredAt` or `observedAt`; whether a row at T counts). Delta v1.3 says only "fold the transitions up to a timestamp". Not built.
7. **Credential presentation, list envelopes and field types, the event types and members beyond `id`, `sequence`, `traceId`, the webhook header names, the payout read** (open since 18 Nov; noema#690).
8. **The OpenAPI document and event schemas** (noema#690): not in the docs repository.

## Pre-work for the next agent
- Exact next step: the Founders answer noema#690 (the OpenAPI first); then items 1 to 7 become fields, not guesses. Until then ENV-05's "Contract suite passes against Tushe" cannot be met: there is no Tushe to observe and no list of routes beyond the two reads.
- Do not touch: the frozen Tushe specification.

# Session 2026-11-25 · ENV-05 · A682: mock-case delivers webhooks through the relay path (IL)
Built from the governing documents only (A682); stop and list what they do not state.

## Built
- `synthetic/mock_case/internal/webhook` (a signer and a sender, standard library only), two bench control routes (`POST /control/v1/webhooks/deliver`, `POST /control/v1/webhooks/{message_id}/replay`), two settings (`MOCK_CASE_WEBHOOK_TARGET_URL`, `MOCK_CASE_WEBHOOK_SIGNING_SECRET_FILE`, both or neither, the secret different from every credential mock-case holds), and the bench API description.
- Tests first (red: no package): the signature equals the documented Standard Webhooks worked example; an independent verifier accepts a delivery; the exact body bytes are sent with their whitespace; a replay keeps the id and the body and takes a fresh timestamp; the same id with another body, a bad id, a non-object or oversize body, a redirect, a timeout, a bad secret or target are refused and nothing is sent; only the bench credential opens the routes; without the settings they answer 404 and send nothing; a relay refusal is reported with its status, not as acceptance. Eleven mutants of the sender and four of the routes: fifteen killed (the sixteenth, the route-level payload check, is covered by the sender's body check behind it).
- The database-backed tests of the `api` package (route list against the description) could not run here (no PostgreSQL); I checked that the description and `BenchRoutes` list the same routes with a script, and CI runs the real test.

## Stated
| Item | Source |
|---|---|
| Standard Webhooks v1: HMAC-SHA-256 over the raw body plus a timestamp, five-minute tolerance, verified with an off-the-shelf library | response of 7 Oct 2026, 4.7 ("Signing is Standard Webhooks, verbatim") |
| The same `webhook-id` on a replay, also the receiver's idempotency key | delta v1.3 section 1; response 4.7 |
| A thin body that carries an id, a type and a URL | response 4.7 (members not named) |

## Not stated: stop and list
1. **The names and shapes of Tushe's event types and body members** (`id`, `type`, `URL` are described, not named). mock-case therefore invents none: the caller supplies the object.
2. **The three header names.** The response says "Standard Webhooks, verbatim" and delta v1.3 names `webhook-id`; `webhook-timestamp` and `webhook-signature` come from the Standard Webhooks specification, not from a Tushe document.
3. **Whether Tushe signs a replay again at the time of the replay.** Built as a fresh timestamp and signature (the five-minute window forces it), same id and same body.
4. **Tushe's retry ladder (eight steps over about 28 hours), its 12-second timeout and the disabled-endpoint rule** are described, not specified as numbers a mock can honour; none is built. Timing and ordering faults stay in the hostility proxy (delta v1.3 section 1).
5. **The signing secret's format and rotation overlap** (24 hours is stated; the format is Standard Webhooks'). Built: `whsec_` plus base64 or raw bytes, one secret, no rotation.
6. **Where the relay lives in the bench profile.** The relay is in the `ci` profile only (netcheck `ONLY_IN`), and mock-case reaches it over `tushe_net`; no Compose or netcheck edge, no setting and no shared secret volume is added here. That wiring is a separate change (SR), and the `ci` profile's relay secret must be the one mock-case signs with.

## Pre-work for the next agent
- Exact next step: the Compose wiring (mock-case's two settings, one shared synthetic secret file for mock-case and the relay, the netcheck edge mock-case to relay on `tushe_net`), after a ruling on item 6.
- Do not touch: the relay's verification (it is the receiver; the sender follows it).
