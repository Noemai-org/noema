# STM-05 RecoverInvestigations (state: implementation local; red pushed as noema#789)

# Session 2026-11-19 17:30 WAT · STM-05

## Session summary
- What changed (PR links): noema#789. `orchestrator.Session` gains AuthnRef, PrincipalType, AssertedBy (edge-only). New package `orchestrator/recovery`: `Activity.Recover` and `StartupPass`, the `Reader` port, `Config` and `LoadConfig` (NOEMA_STM_RECOVERY_IDLE_MS, NOEMA_STM_RECOVERY_BATCH_SIZE, no default), `NewRecoverySession` (the one constructor), `NoDispatch`.
- Why: A204, A377 item 15 (interim, relayed): resume half-run investigations from the Record; spent or attempted means verify by key, never dispatch.
- Risks, and how they are contained: recovery cannot dispatch (activity guard at PLANNED plus `NoDispatch` executor); identity comes only from the stored row; the Record append precedes every move.
- Tests added (REQ, INV, US or EC IDs): REQ-STM-001, REQ-RCV-003, INV-03, INV-07, INV-12, AGENT-07, AGENT-08, EC-E16 (see recover_test.go).
- Regression cases added: kill after spend before EXECUTING; kill when asked to verify.
- Debt entries: none written (outside allowlist). Candidates: fixed recovery turn text because ResumeWrite requires one (fold into a refactor PR); duplicate checks in admit.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: (1) Postgres Reader: idle non-terminal rows by DB clock plus the decision, grant and intent Trust recorded (needs the Trust-side readers of noema#725); (2) register the activity on the worker (noema#721); (3) schedule and TST-05 matrix for G3; (4) Postgres sweeper fallback; (5) the edge sets the three new Session fields.
- Open questions (owner): should the row store role and authn reference (migration)? EL.
- Files to read first: services/core/orchestrator/recovery/recover.go, orchestrator/runwrite.go (ResumeWrite).
- Do not touch: transitions.yaml, the dispatch budget.
