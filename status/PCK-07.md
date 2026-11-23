# PCK-07 pack handoff route (A531): state

State: PR open; see the session section below.

# Session 2026-11-23 WAT · PCK-07

## Session summary
- What changed (PR links): the PR of branch `feat/PCK-07-handoff-route`. `pack.v1` optional `handoff_routes`; the fintech pack routes `NO_RUNNABLE_CAPABILITY` to `PAYMENTS_OPERATIONS` for every node; packcheck check `routing` covers it; manifest pin re-pinned.
- Why: A531 (interim, relayed, not yet committed): the pack declares the handoff route that `Handoffs.Routed` (noema#896) answers from.
- Risks, and how they are contained: additive and optional; a pack without the member routes nothing and Trust answers UNKNOWN as before. The queue choice is a default (see the PR), not derived from a document.
- Tests added (REQ-TRU-004, REQ-AG-002; INV-16): `HandoffRoutes` in `tools/packcheck/test_packcheck.py`; contract fixtures for the schema.
- Regression cases added: the same tests.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL's adapter answers `Routed(tenant, node, reason)` from `pack.handoff_routes` (a route matches when its `reason_code` equals the reason and its `intent_nodes` is absent or holds the node); the orchestrator raises the alert on the reason.
- Open questions (Founders): the queue for `NO_RUNNABLE_CAPABILITY` (PAYMENTS_OPERATIONS chosen as the smallest safe default).
- Files to read first: `contracts/schemas/pack.v1.json`, `tools/packcheck/checks.py` (`check_handoff_routes`).
- Do not touch: `trust/intake`.
