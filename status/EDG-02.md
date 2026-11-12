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

## Session 2026-11-12 16:40 WAT · EDG-02 (fix round)

## Session summary
- What changed: Security Reviewer P1 on `d507cc13`: the ref pattern `[a-z]{2,6}_[0-9A-Za-z_]{2,64}` let `acct_0123456789` through as an unmasked "typed ID". Now each ref key has its contract prefix (`refPrefix`, from `common.v1`) and the value must be exactly `<prefix>_<[0-9A-Za-z]{2,32}>`; an unknown key is a bad event.
- Tests added (AGENT-13, REQ-SEC-003): four bad-event cases (wrong prefix for the key, a key naming no contract ID, a separator in the body, over 32 characters), red before the fix. Three more mutants killed (no body check, loose body pattern, unknown key accepted).
- Risks: a legitimate new ref key needs a line in `refPrefix` (fails closed until added).

## Pre-work for the next agent
- Exact next step: mount the route after noema#486 merges (see the README).
- Do not touch: `refPrefix` without checking `common.v1`.

## Session 2026-11-12 17:30 WAT · EDG-02 (fix round 2)

## Session summary
- What changed: Security Reviewer NOT READY on `2229520f`. (1) blocking: a stream outlived its token; now `min(MaxStream, token's remaining life)` from an injected clock, and an expired token gets 401. (2) blocking: no slow-consumer or concurrency bound; now a write deadline per write, a failed write ends the stream, a global and a per-principal stream cap (retryable 503), and a batch cap. (3) major: the handler checks the customer role itself (same 404). Note 6: `X-Accel-Buffering: no`.
- Tests added (REQ-SEC-003, INV-12, REQ-API-001): token expiry mid-stream, an expired token, a failed write with its deadline, a batch over the cap, per-principal and global caps with the place freed on finish, the handler-side role check, the header. Red first (did not compile: no `Clock`, `WriteTimeout`, `MaxStreams`, `MaxStreamsPerPrincipal`, `MaxBatch`). Ten mutants killed (token life, role check, ignored write error, no deadline, no release, no batch cap, no per-principal cap, no global cap, no expiry refusal); the global-cap one first survived and got its own test.
- Not done, stated in the README: note 4 (per-kind key allow-list, belongs with REC-04 and the Feed), note 5 (only a fake Feed). A rate-limit error code (429) would need an `errors.v3` code; the cap answers the retryable 503 until then.

## Pre-work for the next agent
- Exact next step: mount the route after noema#486 merges, with a real per-tenant Feed (OD-P-82) and the platform masker (noema#457); set all new Options from configuration (no defaults).
- Do not touch: `refPrefix` without checking `common.v1`.

## Session 2026-11-12 23:00 WAT · EDG-02 (fix round 3)

## Session summary
- What changed: Security Reviewer NOT READY on `e87e38c2`, blocking (d): `Numbers` was never masked and its keys only had to match `keyRe`, so `{"account_number": 123456789012345678}` was written whole; `Fields` keys were unconstrained. Now a configured `Vocabulary` per event kind lists the allowed `Fields` keys and the allowed `Numbers` keys with a maximum each (0 to max, at most `MaxNumberDigits` digits); anything else is a bad event. Note 2: a writer that cannot take a write deadline gets 500 and no stream. Notes 3 and 4 and the empty-tenant check are stated in the README.
- Tests added (REQ-SEC-003, AGENT-13): a number under an unlisted key (an account number), over its maximum, negative, under a kind that lists none; a field under an unlisted key or a kind with none; `New` refusing seven bad vocabularies; a writer with no write deadline; each kind its own vocabulary. Red first (did not compile). Mutants killed: no maximum, negative allowed, no number check, no field allow-list, no deadline probe, no digit bound (two forms), wrong kind's vocabulary (two of these first survived and got tests).
- Risks: the vocabulary has no default, so the mounting task must set it (and `MaxNumberDigits` from the shared masking limit once noema#468 lands).

## Pre-work for the next agent
- Exact next step: mount the route after noema#486 merges, with the real Feed, the platform masker, a vocabulary per kind and all options from configuration.
- Do not touch: `refPrefix` without checking `common.v1`.
