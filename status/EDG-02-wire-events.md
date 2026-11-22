# Status · EDG-02 · events route on sse-event.v1 (lane wire_events)

Updated 2026-11-18. State: in review (PR noema#726). Owner: agent lane for EL (STM-01 composition follow-ups). Unreviewed work: EL re-reads it.

# Session 2026-11-18 · EDG-02

## Session summary
- What changed (PR links): noema#726. `services/core/edge/events` rewritten onto `sse-event.v1`: `Event` holds exactly one of the contract's payloads, the handler sets the `event` name and `schema_version` itself, round-trips the payload through its generated strict decoder, requires canonical `Z` instants, and refuses any text the masker would change (cards' own IDs are checked by shape and exempt). Stream faults are `sse-event.v1` error events with `errors.v3` bodies; pre-stream errors are the `errors.v3` envelope. `cmd/core/wire_events.go`: `buildEvents(env, clk, records)` and a Record-backed Feed.
- Removed: the old Event model (Refs, Fields, Numbers), `Vocabulary`, `MaxNumberDigits`, `refPrefix` and its ID table, the `STREAM_FAULT` and `STREAM_UNAVAILABLE` frames, the errors.v2 envelope, the vocabulary tests.
- Why: REQ-API-001 and the `sse-event.v1` contract; the old model could carry free-form members the contract does not define.
- How the feed works: one read-only REPEATABLE READ transaction as the tenant's `record_read` login (`reconstruct.TenantDBs.For`, never another tenant's pool), the whole chain read with `chain.ReadChain` and verified with `chain.VerifyRows` (as the reconstruction does), then a pure derivation. The conversation is the investigation: its correlation is that of its `INVESTIGATION_STATE_CHANGED` rows (`object_ref` = the investigation). It is the caller's only if the correlation has at least one `INPUT_RECEIVED` and every one has the actor `customer:<session subject>`. Each recorded `to` state becomes its public stage (`publicstage.Of`); consecutive equal stages are one event. The cursor is the ordinal among those events, not a chain position, so it reveals nothing about other conversations. A cursor beyond the last event, an absent, foreign or malformed conversation are one answer (`ErrNotFound`, the 404). A terminal state (no exits in the transition table) ends the stream. Every read has its own deadline (`NOEMA_EVENTS_READ_TIMEOUT_MS`) inside the stream's.
- Settings (all required once the Record reader is set, none has a default, AGENT-16): `NOEMA_EVENTS_POLL_MS`, `_MAX_STREAM_MS`, `_WRITE_TIMEOUT_MS`, `_READ_TIMEOUT_MS`, `_MAX_EVENT_BYTES`, `_MAX_STREAMS`, `_MAX_STREAMS_PER_PRINCIPAL`, `_MAX_BATCH`. The values are the Architect's and the Founders'.
- Risks, and how they are contained: (1) the whole tenant chain is read and verified on every poll, as in the reconstruction; correct but costly; a verified-head checkpoint is REC-05 (debt, below). (2) The ownership proof is the `INPUT_RECEIVED` actor, because `INVESTIGATION_STATE_CHANGED` carries no principal; a correlation shared by two principals is refused for both. (3) The generated decoders do not check patterns or bounds (internal/contracts/doc.go): the handler's own checks cover instants, the card IDs and the digit rule, not `minItems` or string lengths; a Feed that builds cards must satisfy those.
- Tests added: `edge/events/events_test.go` (REQ-API-001, REQ-API-002, REQ-SEC-003, INV-12, AGENT-13), `cmd/core/wire_events_test.go` (REQ-API-001, REQ-API-002; INV-12, INV-21).
- Regression cases added: none (new behaviour).
- Debt entries: none written (outside the allowlist); proposed for EL: whole-chain read and verify per poll (REC-05 checkpoint).
- Conflicts raised: none, but see the open question.

## Open question (EL, Architect)
No Record payload holds the content of a clarification card (question, options), a confirmation card (action, target, evidence summary, policy version) or the structured `customer-answer.v1`; `CLARIFICATION_REQUESTED` and `CONFIRMATION_REQUESTED` carry only IDs and reason codes, and `EXPLANATION_RENDERED` carries claim text. The feed therefore emits `progress` events only and never builds a card or an answer from anything else (the overlay must not show an answer the core did not send, and no text may be invented). The handler already carries the other four kinds, validated against the contract. Needed: a decision on where card and answer content is stored (REC-04 payload schemas, or a card store) before those events can be produced. At `COMPLETED` the customer sees stage `answered` and the stream ends with no `answer` event; the answer is still fetched through the investigation response.

## Not run here
The production reader's database path (`readVerifiedChain` against PostgreSQL) is not exercised by a test in this PR: there is no database harness in `cmd/core`, and tests that need pgsodium run in CI only. It uses the same `chain.ReadChain` and `chain.VerifyRows` as `record/reconstruct.Build`, whose database tests cover them. Tests here cover the deadline and no-pool refusals and the whole derivation over rows.

## Pre-work for the next agent
- Exact next step: the final main.go PR calls `buildEvents(env, clk, components.Records)` and mounts `events.Route` behind `session.Middleware` and `session.RequireRole(customer)`; supplies the settings.
- Open questions (owner): card and answer content source (Architect); poll cost (EL).
- Files to read first: `services/core/cmd/core/wire_events.go`, `services/core/edge/events/events.go`, `contracts/schemas/sse-event.v1.json`.
- Do not touch: `compose.go`, `main.go`, `contracts/`.

## Messages
| Date | From | To | Purpose | What the receiver used it for |
|---|---|---|---|---|
| 2026-11-18 | EL (relayed in the lane brief) | this lane | Build `wire_events.go`, EDG-02 | Used for this PR |
