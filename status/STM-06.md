# STM-06 the Interpreter port's session resolver builds its session from the orchestrator Session (state: red pushed; green local)

# Session 2026-11-23 · STM-06

## Session summary
- What changed (PR links): see the PR for branch feat/STM-06-edge-sessions-resolver. `edgeSessions.Resolve` (`services/core/cmd/core/wire_understanding.go`) no longer reads the request's context: it builds `understanding.Session` and the actor kind from the `orchestrator.Session` alone. It refuses, with the one fixed `ErrSessionUnavailable`, a Session that is not whole: empty or whitespace tenant or principal, a role outside the four of ruling A1, an authentication reference that is not `ses_` plus 32 lower-case hex digits, a principal type empty or different from the one `session.PrincipalTypeOf` derives from the role. AssertedBy is passed through as given (empty when not stored). C2 owns this by the Founder's order (the STM lane is EL's).
- Why: A514 (interim): the gateway start fills AuthnRef, PrincipalType and AssertedBy (empty) and `Store.Start` reads `sess.AuthnRef` (noema#882, merged); the resolver swap follows. A recovery has no request, so the resolver cannot depend on one (INV-12, AGENT-10).
- Risks, and how they are contained: a caller that still builds a Session without AuthnRef or PrincipalType is now refused (fail closed). The only non-test builders are the gateway start (`wire_gateway_start.go`, fills all) and `recovery.NewRecoverySession` (fills all). The route-level tests' probe route fills every member from the verified session.
- Tests added (REQ-INT-001, REQ-SEC-004; INV-12, AGENT-10, A514): `cmd/core/edge_sessions_resolve_test.go` (no database): whole Session resolved with no context, source guard that `Resolve` calls no `FromContext`, eleven refusals, issuer passed through and never invented. `TestWithoutTheVerifiedSessionNothingIsCompiled` became `TestWithoutAWholeSessionNothingIsCompiled` (DB test, CI only): the old cases "another tenant" and "another principal" go, because identity is now the Session's, not a comparison with the context.
- Regression cases added: edge_sessions_resolve_test.go.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none for this task; EL's composition passes the Session it builds (gateway start, recovery) to the Interpreter port unchanged.
- Open questions (owner): none.
- Files to read first: `cmd/core/wire_understanding.go` (edgeSessions), `orchestrator/recovery/session.go`.
- Do not touch: `edge/session` (the mapping `PrincipalTypeOf` is the single source).
