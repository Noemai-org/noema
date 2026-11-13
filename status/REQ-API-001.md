# Status: REQ-API-001 public response and SSE contracts (no task ID in the plan yet)

State: draft PR. New contracts only; nothing earlier changes (AGENT-15). No core route is built on them yet.

# Session 2026-11-13 18:00 WAT · REQ-API-001 (public API response and SSE payload schemas)

## Session summary
- What changed: four new contracts in `contracts/schemas/`: `investigation-response.v1` (the response of `GET /v1/investigations/{id}`, PERD §39.2), `customer-answer.v1` (the §13 answer shape), `overlay-card.v1` (the clarification and confirmation cards of §40.1, bound to one ID each), `sse-event.v1` (the data of the five SSE events of §39.2). Fixtures (valid and invalid with expectations), INVENTORY rows, generated Go and Pydantic types, shape refusals, and `tools/contracts/test_public_api.py`.
- Why: the user asked for these schemas ahead of the core routes (EDG-02 events route, the investigation route). There is no task row for them; they trace to PERD §39.2 and §40.1, REQ-API-001, REQ-UI-001, REQ-UI-002 and REQ-SEC-003. The request was given in a session; it is not authoritative until committed (AGENTS §2, item 3), and the Steward should add a task row or change request.
- Risks, and how they are contained: (1) the contracts fix member names and shapes the overlay and the routes must follow; the routes do not exist yet, so nothing running changes. (2) The EDG-02 events handler (noema#496) writes a generic frame (`at`, `fields`, `numbers`, `refs`, with a configured vocabulary) and its own error codes `STREAM_FAULT` and `STREAM_UNAVAILABLE`; neither matches `sse-event.v1` (typed payloads; the `errors.v2` body, whose catalogue has neither code). A follow-up on EDG-02 replaces the frame and maps those codes to `INTERNAL_SAFETY_BLOCK` and `DEPENDENCY_UNAVAILABLE`; the route is unmounted, so nothing breaks. (3) Bounds (`maxLength` 300, list lengths 4 to 8) are frame bounds, not policy; they follow the 160 and 1000 of earlier contracts.
- Tests added (REQ-API-001, REQ-UI-001, REQ-UI-002, REQ-SEC-003): 40 fixtures (11 valid, 29 invalid) against the four schemas; `test_public_api.py` (nine tests): the state list equals `transitions.yaml`, the five event kinds equal PERD §39.2, every free-text member carries the digit-run pattern and a bound, no member of the "Never shown" list or of an internal object exists, every object refuses unknown members, every list is bounded. Six schema mutants killed locally (a confidence member, a missing pattern, a dropped state, a dropped event, an unbounded list, an open card).
- Regression cases added: the invalid fixtures.
- Debt entries: none.
- Conflicts raised: none. Open (AGENT-02): who maps a state to its plain stage string (the overlay or the pack); the contracts carry the state and leave the strings to the UI (PERD §13 lists them as [REC]).

## Pre-work for the next agent
- Exact next step: after review, the EDG-02 follow-up (typed event payloads from the generated types, the error-code mapping) and the investigation route; the UI reads the generated types.
- Open questions (owner): the task row and ID for this contract set (Steward); the stage strings and their source (Architect or pack owner); whether `case_ref_masked` follows A154 (no run over four digits) as built here, while `noema-resolution.v1` allows up to eight (Architect).
- Files to read first: `contracts/schemas/{investigation-response,customer-answer,overlay-card,sse-event}.v1.json`, PERD §39.2 and §40.1.
- Do not touch: the earlier contracts (`clarification.v1`, `confirmation-request.v1`, `errors.v2`, `noema-resolution.v1`).
