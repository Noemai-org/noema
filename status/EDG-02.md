# EDG-02 · the SSE events route (A170)

Task row: proposed by the Steward in CR-2026-11-12-A170-sse-events-task-row (docs#159); owner EL. PERD REQ-API-001 (39.2).

## Session 2026-11-12 · EL

## Session summary
- What changed: `edge/events` (`Handler`, `Feed`, `Event`, `Options`). Tests first (`ce4a2222`, the red head), implementation after.
- Why: ruling A170 (the SSE events now, without waiting for the task row); PERD 39.2 and 40.
- Tests added (REQ-API-001, REQ-API-002, REQ-SEC-003, INV-12, AGENT-13): thirteen in `events_test.go` (exact frames; the feed asked with the token's principal and tenant; Last-Event-ID; the same 404 for foreign, missing and malformed; 503 before the stream and a safe error event after; masking and typed IDs; nine bad-event rules; keepalive, client leaving and the stream deadline; constructor refusals; only a customer; a writer that cannot flush). Fourteen mutants killed (cursor order, no mask, refs masked, no ref pattern, not-found as 503, no size bound, control characters, no keepalive, any Last-Event-ID, no conversation pattern, Done ignored (the test hangs, which is the failure), resume from zero, no member bound). Two first survived or did not compile and were redone compile-safe; the member bound had no test until a 17-member case was added.
- Debt: none new. The `Feed` over PostgreSQL and the mount line are next steps, not shortcuts.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after noema#486 merges, add the route to `apiRoutes` with role customer and a `Components.Events` feed; build the Feed from the per-tenant reader (OD-P-82) over the `investigation` table (noema#490) and the committed Record events; pass the platform redactor (noema#457) as `Mask`.
- Do not touch: `edge/session`.
