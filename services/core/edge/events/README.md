# edge/events

**What it does.** The server-sent events route of the public API (EDG-02, ruling A170): `GET /v1/conversations/{conversation_ref}/events` streams the `progress`, `clarification`, `confirmation`, `answer` and `error` events of one conversation (PERD 39.2). It writes only what a `Feed` returns as committed state (PERD 40). The feed is asked as the verified session's principal and tenant (INV-12); a conversation that is not the caller's is the same 404 as one that does not exist (REQ-API-002). A free-text field goes through the injected masker (A154); a ref is exempt from masking only if it is exactly the contract ID of the type its key names (`refPrefix`: key to `common.v1` prefix, body `[0-9A-Za-z]{2,32}`); a key outside that table, or a value of another shape (for example `acct_0123456789`), is a bad event and nothing of it is written; numbers and instants are typed, so no exemption list is needed here. `Last-Event-ID` resumes after a cursor. A bad event ends the stream with `event: error` / `STREAM_FAULT` and writes nothing of it; a feed failure after the stream began is `STREAM_UNAVAILABLE`. A keepalive comment is sent when a poll returns nothing. Every limit (poll interval, stream limit, event bytes) is configuration.

**REQs.** REQ-API-001, REQ-API-002, REQ-SEC-003. SoT: INV-12, AGENT-13.

**Not here.** The `Feed` over PostgreSQL (it needs the per-tenant reader, OD-P-82: a file-mounted read-only DSN per tenant; the route stays unmounted until configured); the mount line in `cmd/core` `apiRoutes` (after noema#486); the platform redactor as the masker (noema#457; the masker is injected); the clarification and confirmation producers.

**Tests.** `go test ./edge/events/` from `services/core/` (no database).
