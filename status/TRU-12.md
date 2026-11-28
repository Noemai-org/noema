
# Session 2026-11-26 · TRU-12 · A710, A724 (C1, interim, relayed): the confirmation store's Record events

## Session summary
- What changed: `trustpg2.RequestEvents` now takes the store's transaction (`Received(ctx, tx, e)`, `Expired(ctx, tx, e)`); `Requested` is out of the interface (the Recorder's Writer appends CONFIRMATION_REQUESTED in the CONFIRM decision's transaction, A552; the test-only `Open` keeps it through an optional interface). `trustpg2.RecordEvents` is the real implementation over `chain.Appender` as `core-trust`: CONFIRMATION_RECEIVED with the actor `<kind>:<approver>` and CONFIRMATION_EXPIRED as the core's own event. `AnswerInput` carries `ApproverKind` (the session's role, never a request field, INV-12; refused unless `^[a-z]+$`, and the appender refuses a kind outside the Record's actors table); `cmd/core` passes `string(p.Role)`.
- Why: A710 (RequestEvents appender, C1), A724 (a transaction parameter and the approver actor kind).
- Risks: (1) the store and the Record events share one login, so the Trust login must be allowed to append `record_event` as `core-trust`; the tests run on `trust_rw`. (2) Nothing wires `Requests` into `cmd/core` yet (the gap `needRequestEvents` stays until the composition does it).
- Tests added (REQ-TRU-009; INV-12, INV-25; EC-E16): `record_events_test.go` (4 tests on real PostgreSQL with the real appender: the event is in the answer's transaction; a refused append stores no answer; a kind that is not a principal kind is refused; an expiry is the core's own event). Regression cases: the red at `b2bdb2940`. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the composition builds `trustpg2.Requests{Events: trustpg2.RecordEvents{Appender: ...}}` and removes the gap text `needRequestEvents` from `cmd/core/wire_decisions.go`.
- Do not touch: `trust/recorder` (it appends CONFIRMATION_REQUESTED).
