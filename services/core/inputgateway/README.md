# inputgateway

**What it does.** The core's side of the Input Gateway (UND-02, ruling A161). One request body plus the identity of the authenticated session go in. On a valid, new input it appends `INPUT_RECEIVED` to the Record as `core-edge` and only then hands the orchestrator the turn, with the reference of the recorded event. A repeated `request_id` with the same body appends `INPUT_DEDUPLICATED` and returns the original result. The same `request_id` with another body is refused with 409 and appends `SECURITY_EVENT` of kind `REQUEST_ID_REUSED`. If any append fails the input is refused and nothing proceeds (EC-E16).

**REQs.** REQ-INP-001, REQ-INP-002, REQ-INP-003, REQ-AUD-001. SoT INV-11, INV-12, INV-30. Ruling A161.

| File | What |
|---|---|
| `gateway.go` | `Gateway.Accept(ctx, Identity, body)`: identity check, the closed body (only `request_id`, `channel`, `text`, `locale_hint`, `voice`, `conversation_ref`; one object; no repeated, unknown or null member; `request_id` a lower-case UUID; `channel` `text` or `voice`; non-empty `text`), the hash of the canonical body, the lookup, one append, then the turn. Constructors only: `New(Config)` |
| `record.go` | `RecordRecorder` appends the three events through `chain.Appender` on the tenant's **core-edge emitter login**, one transaction per event. `RecordIndex` finds an earlier input by reading the tenant's chain through `record_read_chain_t` as the tenant's `record_read` login |
| `handler.go` | `Gateway.Handler(resolve)` serves `POST /v1/conversations`: 202 for a new input, 200 for a retry, 400 `INPUT_INVALID`, 401, 409 `REQUEST_ID_REUSED`, 413, 503 `DEPENDENCY_UNAVAILABLE`, in the errors.v2 envelope. `resolve` returns the session identity; nothing about identity is read from the query, the headers or the body |

**The events.** All three are written by `core-edge`, with actor `<kind>:<principal>` and the tenant of the login (the database refuses another), object reference `req_` plus the request id's hex digits:

| Event | Payload | Correlation |
|---|---|---|
| `INPUT_RECEIVED` | `channel`, `request_id`, `locale` (only when the client sent `locale_hint`) exactly as `record/reconstruct/project.go` reads them, plus `input_hash` (never read by the reconstruction, never the text) | a new `corr_…` minted here |
| `INPUT_DEDUPLICATED` | `request_id`, `original_event_id` | the original input's |
| `SECURITY_EVENT` `REQUEST_ID_REUSED` | `request_id`, `original_event_id`, `input_hash` of the new body | the original input's |

**What a retry gets.** The stored response reference of REQ-INP-003: the original `INPUT_RECEIVED` reference and correlation id (`correlation_id`, `input_event_id`, `input_seq`, `deduplicated: true`). The answer to the first request is read by correlation id; no response is stored here.

**Scope of the check.** Same tenant, same principal, within the window (REQ-INP-003 says 24 hours, from configuration; `RecordIndex.Window`). "Same body" means the same members and values, whatever the whitespace and member order.

**For EL (composition root).** Build `Config` with `RecordRecorder{Pool: <core-edge login pool>}`, `RecordIndex{Pool: <record_read login pool>}`, `Start` (the orchestrator seam: it receives `Turn`, which carries `Input`, the recorded `INPUT_RECEIVED` reference), the ID source, the body bound and the window from configuration, then mount `Handler(resolve)` behind the session check and the `customer` role gate. `Identity` needs the session's tenant, subject, principal kind and session id; the edge's session does not carry the kind or the session id yet (noema#454, item 3), so the mapping is the composition root's.

**Open, stated (also in `status/UND-02.md`).**
- `RecordIndex` has no table to look in (the Record is the only store, INV-30), so each lookup reads the tenant's `INPUT_RECEIVED` events in the database and filters them there; the cost grows with the tenant's chain. A keyed lookup function is the fix and needs a migration; not made here.
- The "one input per request id" guarantee across concurrent requests is a lock inside the process. It holds for one core process (the POC runs one). Two processes could both append `INPUT_RECEIVED`; a unique key in the database would close it.
- The scope is the principal, not the session: the Record carries the actor, not the session id.
- Every error body carries the correlation ID of the **request**: the one the edge's session middleware already put on the `Noema-Correlation-Id` response header (PERD 39.1), so the body, the header and the logs agree; a value that is absent or not of the contract's shape is replaced by the edge's own stand-in `corr_NONE`, never echoed, and the handler mints no ID on a failure. The `correlation_id` of a success body is the **investigation's** (minted by the gateway, never taken from the client's header, so a client cannot choose which chain an input joins).
- If `Start` fails after `INPUT_RECEIVED` is recorded, the client gets 503. A retry of the same body appends `INPUT_DEDUPLICATED` and then calls `Start` again for the same recorded input (ruling A168), and is answered with the original result only if that `Start` succeeds; if it fails, the client gets 503 again. So `Start` is called on every delivery of an input and **must be idempotent per `Turn.Input`**: the investigation store of noema#490 is (`Store.Start` finds the earlier investigation of a recorded input). A request id reused with another body, and a retry whose `INPUT_DEDUPLICATED` cannot be appended, never reach `Start`. Resuming a run that started and then failed inside the orchestrator is EL's (STM-01), not the gateway's.
- The text rules (NFC, trim, 2,000 characters, `is_final`) are the model service's gateway, not here.

**Tests.** `go test -race ./inputgateway/` from `services/core/`; they need PostgreSQL (the shared test server) and run against the real Record with a `core-edge` emitter login and a `record_read` login. Nineteen mutations of the gateway and recorder were run by hand and each is killed by a named test (see `status/UND-02.md`).
