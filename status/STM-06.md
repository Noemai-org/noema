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

# Session 2026-11-26 16:09 WAT · STM-06

## Session summary
- What changed (PR links): see the PR for branch test/STM-06-gateway-seam-identity (tests only; follows noema#919, which merged on 26 Nov 2026 without a seam-level identity test). Three new files in `services/core/cmd/core/`: `gateway_seam_world_test.go` (the chain and its doubles), `gateway_seam_identity_test.go` (start path, request cannot change the identity, refusals, context), `gateway_seam_recovery_test.go` (recovery session from the stored row). No production file, contract, migration, dependency or threshold changed.
- Why: the Founder's order of 26 Nov 2026 ("add the gateway-seam identity test on #919"). The seam is where the gateway start builds the orchestrator Session from the verified token (`turnStarter.Start`) and `edgeSessions.Resolve` turns it into the Interpreter's session (INV-12, AGENT-10, A514). The existing tests proved each half apart: `edge_sessions_resolve_test.go` the resolver on built Sessions, `wire_gateway_start_test.go` the start with a faked Turn, `gatewayidentity_test.go` the identity of a signed token. None followed one verified token to the Interpreter.
- Risks, and how they are contained: characterisation tests, so there is no red run (the behaviour exists). Their teeth are hand mutants (32 applied to the production files, all reverted, `git status` shows only the three new files): 31 killed, 1 survived and it is equivalent (`Role: t.PrincipalKind` in Start, which refuses any kind but customer first). Doubles: the Record of the inputs and the investigation store (nothing the seam reads back), the runner (it makes the one call the orchestrator's runner makes first, `Interpreter.Interpret`) and the core's Interpreter (a counting double). The real parts are the edge's token verification, middleware and role gate, the Input Gateway, the production Start, the production resolver and the real port.
- Tests added (REQ-INT-001, REQ-SEC-004; INV-12, AGENT-10, A514): eleven, none needs a database. The Interpreter is handed the verified token's tenant, principal, role, reference, type and issuer (two tokens); headers, query or a body naming another identity change nothing (the body is refused 400 before anything is recorded); a non-customer token never reaches it; the issuer is carried from the Turn and never invented; refusals of Start and of a failing store never reach it; a verified subject of another form is refused by the port; sixteen Sessions that are not whole are refused by the port with a counting double at zero; a context that carries another verified session neither completes nor replaces a Session; a recovery session equals the start's session member for member; it is built from the stored row alone for the four roles; it is refused whole (zero Session) for fifteen rows that lack or malform a member.
- Regression cases added: the three files above.
- Debt entries: none. A DB round trip (Start writes `investigation.asserted_by`, `SessionOf` reads it, recovery rebuilds the session) was not added: the storage half is proved on a real database by `orchestrator/investigations/issuer_column_test.go` and `session_columns_test.go` (CI only), and this seam does not need a database. The row here is the `RecordedSession` value those tests prove `SessionOf` returns.
- Conflicts raised: none. Noted, not a conflict: `edgeSessions.Resolve` passes an empty issuer through (A514 text) while the port's `Session.Valid()` refuses it with `interpreterport.ErrSession`; the tests pin both, so the port is the guard that fails closed on a missing issuer.

## Pre-work for the next agent
- Exact next step: none for this task. If a DB-backed composition test of start, row and recovery is wanted, it belongs with EL's composition (STM-01) and needs a signed INPUT_RECEIVED in the Record for `Store.Start`.
- Open questions (owner): none.
- Files to read first: `cmd/core/gateway_seam_world_test.go`, `cmd/core/wire_gateway_start.go` (Start), `cmd/core/wire_understanding.go` (edgeSessions), `orchestrator/recovery/session.go`.
- Do not touch: `edge/session` (the mapping `PrincipalTypeOf` is the single source); the tests' oracle is the edge's own verifier, keep it independent of the code under test.
