# Session 2026-11-23 13:05 WAT · TRU-12 (C1, A550: the confirmation request-by-id read)

## Session summary
- What changed (PR links): tests first (RED), GREEN follows in the same PR. A550 (`DEC-2026-11-23-architect-rulings-a542-a553`, interim): C1 owns the confirmation request-by-id read. The route (`cmd/core/confirmations_route.go`) takes a `ConfirmationRequests.Lookup(ctx, tenant, requestID) (confirm.Request, bool, error)` and `trustpg2.Requests` had only `Latest` (by principal and intent), so nothing implemented it. `Requests.Lookup` reads one request by its ID for the session's tenant: another tenant's, an unknown ID and a request the store does not hold are the same answer, not found; a malformed tenant or ID and a context with no deadline are `ErrRequestForm`; it writes no event.
- Not here: the rest of lane 5 (A547), the composition of the Execution Controller (ledger, dispatcher, receipt signer, Verifier, Bindings) and the Outcome request builder. Inventory in the PR body.
- Tests added (REQ-TRU-009, REQ-TRU-003, REQ-SEC-004; INV-12): `orchestrator/portadapters/trustpg2/lookup_test.go`.

## Pre-work for the next agent
- Exact next step: EL wires `trustpg2.Requests` as `ConfirmationRequests` in the composition (cmd/core).
- Files to read first: `orchestrator/portadapters/trustpg2/requests.go` (`Latest`, `Lookup`), `cmd/core/confirmations_route.go`.
