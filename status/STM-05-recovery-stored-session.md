# STM-05 recovery builds its session from the stored row (state: red pushed; green local)

# Session 2026-11-21 · STM-05

## Session summary
- What changed (PR links): this PR (see the PR list for branch feat/STM-05-recovery-stored-session). `recovery.New` takes a `SessionReader` (investigations.Store.SessionOf). `NewRecoverySession(c, rs)` is the one constructor: tenant and principal from the row, Role and AuthnRef from the stored principal_role and authn_ref, PrincipalType from `session.PrincipalTypeOf` (new, the edge's one mapping), AssertedBy empty. A legacy row (ErrNoRecordedSession) is never resumed: outcome NO_RECORDED_SESSION, counted in `Report.NoRecordedSession`, summed by `TotalNoRecordedSession`. A role outside the A1 set or a malformed authn_ref is Refused and counted in `Report.Refused`.
- Why: A490 (interim, relayed, recorded in docs#219): recovery reads the stored session, never invents a role, and keeps legacy rows visible.
- Risks, and how they are contained: Trust's technical-authority step now sees the original role in a recovery; the role is the one the edge verified at Start, stored by migration 0036. No request value reaches the session (property test).
- Tests added (REQ, INV, US or EC IDs): REQ-VER-004, INV-12, INV-30, AGENT-10, A149, A490 (recover_test.go, edge/session/principaltype_test.go).
- Regression cases added: legacy row not resumed and counted.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Postgres Reader and the worker registration pass `*investigations.Store` as the SessionReader; the caller logs `TotalNoRecordedSession` after StartupPass (no metrics seam exists yet: platform/telemetry is empty).
- Open questions (owner): none.
- Files to read first: orchestrator/recovery/session.go, recover.go.
- Do not touch: transitions.yaml, the dispatch budget, migration 0036.
