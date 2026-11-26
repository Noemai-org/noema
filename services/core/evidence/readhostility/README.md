# evidence/readhostility

**What it does.** Tests only, no production code. They show what the Evidence read path (`StandinClient` and `portread.Reader`, items statused by `layer.AssignStatus`) makes of each fault the hostility proxy produces. Task EVD-05, the hostility-proxy part (A717; the Tushe-contract part keeps ENV-03). 26 Nov 2026. SYNTHETIC (AGENT-25).

**REQs.** REQ-CTX-008, REQ-CTX-002, REQ-TST-004, REQ-ENV-004; INV-07, INV-19, INV-23; US-21; EC-S19 (schema drift), EC-T09 as a read (near timeout), EC-S12 and EC-S19 for stale reads (partly, see below).

**Where the faults come from (AGENT-12).** `testdata/read_hostility_recordings.json` is made by the real hostility proxy over a real connection, from real fault plans, by `TestReadHostilityExchanges` in `benchmark/hostility_proxy/internal/proxy/record_read_hostility_test.go`. That test regenerates the file in memory and fails if the committed copy differs; re-record with `NOEMA_RECORD_READ_HOSTILITY=1 go test ./internal/proxy -run TestReadHostilityExchanges` from `benchmark/hostility_proxy`. 22 families by 3 reads (ledger, switch, destination) = 66 exchanges. The source behind the proxy is a test-only upstream in the stand-in's wire shape (a test fails if its members drift from the real stand-in's recordings). The tests replay each exchange to the real client: same status, `Content-Type`, `Retry-After` and bytes; the recorded hold (`hold_ms`) for the timing effects; the connection cut where the proxy gave no answer. A transport fault cannot be a byte recording, so the replay of timing is the one place a test server behaves instead of the proxy; the proxy's own tests (`fault_test.go`) show the proxy does it.

| Family | Effect | What the read path does |
|---|---|---|
| refused | `HTTP_5XX` 500, `SERVICE_UNAVAILABLE`, `HTTP_429` with and without `Retry-After` | UNAVAILABLE, no payload, the refusal's bytes hashed, one request, no retry |
| corrupted or drifted | truncated, invalid JSON, wrong `Content-Type`, empty body, `SCHEMA_DRIFT` add, remove, rename, unknown enum | UNKNOWN (MALFORMED), no payload; the unknown enum is never a state |
| clock skew | `CLOCK_SKEW` +900 s and -900 s | KNOWN; the skewed times are recorded as sent, never corrected |
| near timeout | `LATENCY`, `NEAR_TIMEOUT_BEFORE`, `NEAR_TIMEOUT_AFTER` (hold 150 ms) | KNOWN within the read's deadline; beyond it UNAVAILABLE (TIMEOUT), no bytes, the late answer never used |
| silence | `TIMEOUT_BEFORE_COMMIT`, `TIMEOUT_AFTER_COMMIT`, `DROP_RESPONSE` | UNAVAILABLE, no bytes, no payload |
| stale | `STALE_READ` 5 s after a change upstream | the earlier state was not final, so UNKNOWN; never KNOWN |

In every case the other two reads of the transaction are still made and are KNOWN, and `TestNoModelIsInTheReadPath` fails if the packages driven here reach a model, explanation, provider or voice package or a provider SDK.

**What it does not show (stated, not hidden).**
- Retry-After and the [REC] 100, 300, 900 ms backoff: no read retries and no backoff exist in the read path (the retry budget is OD-P-112 and agents never set one, AGENT-16). The tests show the no-retry default. Go's HTTP transport replays an idempotent GET once on a connection that was already in use if the source drops it without answering, so a dropped read can reach the source twice; the tests allow two there and one everywhere else.
- Pagination (`PAGINATE`, EC-E13): the stand-in's transaction reads have none; its candidates search is the Candidates API's (another task), and the case lists need a route table for mock-case that the proxy does not have.
- Event feed faults (`DUPLICATE_WEBHOOK`, `REORDER_EVENTS`, `DROP_WEBHOOK`, `LATE_EVENT`): the read path here does not read events; they belong with the event feed reader and the Tushe-contract part.
- A stale answer whose earlier state WAS final looks like a live answer: `AssignStatus` reads only the retrieval time. See the PR, "decisions needed".
- Item persistence and the `EVIDENCE_READ` event: `layer.Builder` tests (database, CI) own them; here a recorder keeps the observation and the status is the function the Builder applies. The attempt count the PERD names in `EVIDENCE_READ` is not recorded anywhere yet.

**Tests.** `go test -race ./evidence/readhostility/` from `services/core/` (no database); `go test -race ./...` in `benchmark/hostility_proxy/` regenerates and compares the recordings.
