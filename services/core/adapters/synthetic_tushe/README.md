# adapters/synthetic_tushe

Clients of the synthetic enterprise behind the interfaces in `adapters` (PERD §64.2). **Built:** `CaseClient`, the client of `mock-case` (REQ-ENV-001, REQ-EXE-007, REQ-ENV-008). **Not built:** any client of hosted Tushe (paused; see `../README.md`).

## CaseClient

- Talks to the case listener with the **runtime** credential, read from a file (`NewCaseClient(baseURL, tokenFile, httpClient)`; the content is never logged, returned in an error or sent to another host; redirects are not followed). It has no code for the truth or control listeners and never holds the bench credential; a test fails if such a path appears in the source.
- Sends exactly one request per write and never retries (REQ-EXE-006). A write needs a positive timeout; a read needs a context deadline.
- Classification (PERD §27.1 E10, REQ-EXE-007), raw code kept beside it:

| mock-case answers | Class | Raw code | Note |
|---|---|---|---|
| 201 with a case | `ACK_2XX` | `201` | `ExternalAck` is the case reference |
| 200 replay of the same payload | `ACK_2XX` | `200:REPLAY` | `ExternalAck` is the existing reference |
| 200 `{"status":"PENDING"}`, nothing created | `ACK_2XX` | `200:PENDING` | no reference; Outcome decides (EC-E03) |
| 409 `IDEMPOTENCY_CONFLICT` | `REJECTED_4XX` | `409:IDEMPOTENCY_CONFLICT` | `KeyCollision` is true (EC-E06); never acknowledged |
| 409 `VERSION_MISMATCH` (route) | `VERSION_CONFLICT_409_412` | `409:VERSION_MISMATCH` | `CurrentVersionToken` carries the current token |
| 429 | `REJECTED_4XX` | `429:RATE_LIMITED` | `RetryAfter` if sent |
| other 4xx | `REJECTED_4XX` | `<status>:<CODE>` | |
| 5xx | `SERVER_5XX` | `<status>:<CODE>` | committed or not, it is never read as failure |
| timeout, dropped connection, reset, no response | `TIMEOUT`, `NO_RESPONSE`, `CONNECTION_RESET` | same | the write may have happened |
| a 2xx the adapter cannot read | `NO_RESPONSE` | `<status>:UNRECOGNISED` | schema drift is unknown (EVD-05) |
| bad input to the adapter | `ADAPTER_NON_RETRYABLE_DEFECT` | `ADAPTER:INVALID_INPUT` | nothing is sent |

- Reads return a `CaseRecord` with its version token, or a `CasePage` with `Complete`; a transport failure is `UNAVAILABLE`, never `NOT_FOUND`; unknown fields are `MALFORMED`.

## Tests

`case_client_test.go` replays 19 exchanges recorded from the real mock-case (real PostgreSQL, faults from real FaultPlans: `HTTP_200_SEMANTIC_PENDING`, `IDEMPOTENCY_COLLISION`, `HTTP_5XX`, `HTTP_429`, `DROP_RESPONSE`, `TIMEOUT_AFTER_COMMIT`, `PAGINATE`, `CONCURRENT_MODIFICATION`, `INJECTION_PAYLOAD`) in `testdata/mock_case_recordings.json` (provenance inside). Each replay fails if the adapter's request differs from the one the real mock-case accepted. Mutation check: seven deliberate breaks of the adapter were all caught.

## Constraints and open

- SYNTHETIC only; no real credential or data.
- Open: a live CI run against a running mock-case (workflow change); the Execution bridge; complaint and evidence operations.
