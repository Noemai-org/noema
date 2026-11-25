# STM-07 gateway issuer (on noema#925)

State: red `c877f5d1` read in CI, green `1ff3865f` pushed on #925; CI green at that head; SR READY at `1ff3865f`. PR B (recovery refuses a row with no issuer) follows after #925 merges.

# Session 2026-11-24 12:00 WAT · STM-07

## Session summary
- What changed (PR links): commits on top of noema#925 (C2 consented, A610): `inputgateway.Identity.AssertedBy` (the verified issuer), refused by the identity check when malformed (`^[a-z][a-z0-9_-]*$`); `gatewayIdentity` copies `session.Principal.AssertedBy`; `turnStarter.Start` refuses an empty or malformed issuer (`ErrStartTurn`, before the store is asked) and sets `orchestrator.Session.AssertedBy` from the turn.
- Why: A584, A592, A514 (amended); INV-12, AGENT-10. The issuer is the edge's verified one, never a request value.
- Risks, and how they are contained: an empty issuer passes the gateway's identity check (so input is still recorded) and fails at Start, as ruled; the INPUT_RECEIVED event does not gain the issuer (that would need a contract change; not done).
- Tests added (REQ, INV, US or EC IDs): INV-12 tests in cmd/core (start passes issuer; empty/malformed refused with nothing stored; gatewayIdentity copies it; header, query and body cannot set it) and inputgateway (malformed issuer is ErrNoIdentity). DB-backed tests run in CI only (pgsodium).
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: PR B after #925 merges.
- Open questions (owner): none.
- Files to read first: services/core/cmd/core/wire_gateway_start.go, gatewayidentity.go.
- Do not touch: C2's migration 0039 and store.go.
