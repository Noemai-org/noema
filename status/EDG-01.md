# Status · EDG-01 (core edge, session slice)

## Session 2026-11-04 · slice 1: the session verifier and middleware
## Session summary
- What changed: `services/core/edge/session/{session.go,http.go}` and their tests (new); the `edge` package doc is unchanged.
- Why: the Architect's order of 4 Nov 2026 (item 2): EDG-01, the smallest slice that lets the benchmark open a session, plus the session provider. G1 path: the public API (US-01, US-06) needs an authenticated session (REQ-API-004).
- What it does: `Verifier.Verify` checks an ES256 (P-256, SHA-256) JWT: signature against a key chosen by `kid`, audience, tenant (one of the deployment's), expiry, issue time (not in the future), a maximum lifetime (an input, REC 15 min), known roles; the principal is `sub` only. Every failure is one error, `ErrInvalid`. `Middleware` requires `Authorization: Bearer`, answers 401 `UNAUTHENTICATED` (no credential) or `SESSION_INVALID` (anything else, one fixed message, so a forged and an expired token look the same) in the §39.1 envelope with `Noema-Correlation-Id`, and puts the principal in the request context. No new dependency (standard library only).
- Open (AGENT-02): the claim names are not defined anywhere in the SoT or PERD. Smallest safe default used: `sub`, `aud`, `exp`, `iat`, `tenant_id`, `roles` (`customer`, `operator`, `approver`). Evidence that would invalidate it: the enterprise IdP contract or the benchmark's IdP naming them differently. **Needs an [OPEN] entry in `docs/open-decisions.md` (the Repo Steward adds it; I do not hand-edit the register).** The 15-minute lifetime and the 5-minute freshness for sensitive operations are PERD [REC] and are inputs, not constants here.
- Risks, and how they are contained: not wired into `cmd/core` yet, because there is no public endpoint to protect (PERD §39.2 lists them; adding a "whoami" route would add an endpoint `/contracts` does not define). Wiring (a public-key file from configuration, the tenant list, the route table) arrives with the first public route. No rate limit, `request_id` deduplication or ops/audit route roles yet (the rest of EDG-01's done-when).
- Tests added (REQ-SEC-001, REQ-API-004, REQ-API-002, INV-12): 27 kinds of bad token each rejected (forged, flipped bit, swapped payload, alg none, alg HS256, unknown or missing kid, wrong audience, wrong or missing tenant, missing or empty sub, no exp, expired, future, over-long, unknown role, no roles, malformed); exact time boundaries; 20,000 seeded token mutations, none accepted; degenerate signatures (zero, curve order, one); the verifier refuses to start without keys, audience, tenants, lifetime or clock, or with a non-P-256 key; the middleware: 401 shapes, forged and expired indistinguishable, handler not run, correlation ID kept only when well formed, no echo of a hostile header, the principal only from the token. Seen failing first (did not compile). Mutation: 8 of 8 on the verifier and 5 of 5 on the middleware caught after two tests were added for survivors.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Not built (named)
- The synthetic IdP and the benchmark's session provider (next PR in this task): a tool that issues these tokens from a private key held outside the core, and `benchmark/runner`'s `DirSessionProvider` replaced or fed by it. Tokens live 15 minutes, so a directory of static files cannot serve a long run.
- Per-session rate limits, `request_id` deduplication, ops/audit route roles, reconstruction and verification endpoints behind the edge (EDG-01's done-when).

## Pre-work for the next agent
- Exact next step: the issuer (`synthetic/idp`) and the provider; then wire `session.Middleware` into `cmd/core` with the first public route.
- Do not touch: `noema#94`, `noema#90`.
