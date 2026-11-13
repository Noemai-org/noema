# Status · TST-05, the ledger boundary (Core Engineer 1) · noema#506

Updated 2026-11-13. Asked by the Integration Lead in noema#506 for the crash matrix of noema#498 (S9.2, the kill inside the receipt transaction).

## Session 2026-11-13 · the boundary inside Ledger.Complete
### Session summary
- What changed (PR links): noema#516: `execution/ledger/boundary.go` (`ledger.Point`, `PointInCompleteTxBeforeCommit`, `Points()`, `ErrCrash`), `Ledger.Boundary func(Point) error` (nil in production), and the call at the end of `Complete`'s transaction, after every write of the receipt (the VOID, RecoveryRequest and GRANT_VOIDED of a stale-version refusal included) and before the commit.
- Why: `controller.PointInCompleteBeforeCommit` fires before `Ledger.Complete` opens its transaction, so the rollback of a half-written receipt was not crash-testable. The hook is the ledger's own `Point`, as the verifier has its own, so the matrices over `controller.Points()` do not change.
- Tests, failing first (red on `1a84bf91`: `Boundary undefined`): `boundary_test.go`: a kill at the boundary leaves no receipt, no idempotency update, no outbox event and no RECEIPT event, keeps the spend and the dispatch attempt, and the sweeper then writes one OUTCOME_UNKNOWN and dispatches nothing; the stale-version receipt keeps neither the VOID nor the RecoveryRequest nor GRANT_VOIDED; a NOWAIT lock probe from another connection proves the hook runs after the writes. Three mutants, each caught: the hook before the writes, its error swallowed, the hook skipped for the stale-version receipt.
- Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the Integration Lead adds the kill point to the shared S9 list and the matrix (`outcome_e2e`), setting `led.Boundary` in the crash child.
- Do not touch: `controller.Points()` (the matrices that range over it).
