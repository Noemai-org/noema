# STM-07 recovery refuses a row with no verified issuer (PR B of A592)

State: red `72b2b09c9` (tests only) and green committed locally on `feat/STM-07-recovery-refuses-no-issuer`; nothing pushed yet. Follows noema#925 (merged: migration 0039, `Store.Start` writes the issuer, `SessionOf` returns `AssertedBy`).

# Session 2026-11-25 15:00 WAT · STM-07

## Session summary
- What changed (PR links): PR B of A592 (number to be filled when opened). `recovery.NewRecoverySession` copies the row's `AssertedBy` into the rebuilt session and refuses a row whose issuer is empty or outside the asserted_by shape (`^[a-z][a-z0-9_-]*$`, the same as migration 0039's CHECK). The sweep already counts that refusal in `Report.Refused`, so no new counter and no new outcome. No migration, no store change, no contract change.
- Why: A584 (recovery refuses a row without the issuer; legacy rows never get an invented one), A592 (PR B only after PR A merges; legacy rows are refused or left for a person), A514 amended. INV-12, AGENT-10.
- Risks, and how they are contained: PR A is merged, so the stack's own new rows carry an issuer and the refusal cannot refuse a row the running stack just wrote (the window A592 removed). Rows created between the 0036 and 0039 stacks are refused and counted; they need a person (named in A592). A row with no role or authn reference stays `NO_RECORDED_SESSION` (A490), unchanged.
- Tests added (REQ, INV, US or EC IDs): REQ-VER-004, INV-12, AGENT-10, A584, A592: `TestRecoverySessionRefusesARowWithNoIssuer`, `TestRecoverRefusesARowWithNoIssuerAndCountsIt`, `TestRecoverNeverReachesAPortForARowWithNoIssuer`, `TestStartupPassCarriesTheRefusedNoIssuerCount`; the three session-shape tests now assert the stored issuer reaches every port.
- Regression cases added: the four above.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL reads the red job, then pushes the green.
- Open questions (owner): whether a legacy row with no issuer should be repaired by a person-run tool or stay refused (A592 says "refused or left for a person"; no tool is built; EL).
- Files to read first: services/core/orchestrator/recovery/session.go, recover.go.
- Do not touch: migrations/0039, investigations/store.go.
