# Status · TRU-12 · The RouteScopes production reader (A625, A639)

Updated 2026-11-24. Owner: Engineering Lead (EL); built by a helper session under EL's allowlist. Rulings A625 and A639 are interim, relayed, recorded in docs#241, not yet committed.

## State
- Red pushed (feat/TRU-12-routescopes); green committed locally, not pushed. No PR opened.
- Not built, listed: the CreateGrantID wiring in the ESCALATING handoff. No handoff code that starts the escalation exists on main (`EscalationStarter` is noema#945's). A625 gives no decision for a handoff that does not exist, so nothing was invented.

## Session 2026-11-24 · TRU-12

### Session summary
- What changed (PR links): none opened. `investigations.Store.SessionByCorrelation` (new file `bycorrelation.go`); `trustport.RouteScopeReader` (`routescopes.go`), `trustport.RouteSessionHandoff` (`routehandoff.go`); `RouteRequest.Session` and the `RouteScopes.ScopeFor` signature (`routedecider.go`, 4 lines); the A504 guard amended (A639).
- Why: A625 (the orchestrator reads the investigation with its own login and hands the session to Trust as typed data; Trust never reads it, BD-05), A639 (the guard amended narrowly, same change as the field).
- Risks, and how they are contained: noema#945 changes `RouteDecider` (DecideRouteAfter): expect a textual conflict in `routedecider.go` (the `ScopeFor` call and the `RouteScopes` interface). The edit there is the interface line, the `RouteRequest` field and one call argument.
- Tests added (REQ, INV, US or EC IDs): REQ-TRU-010, REQ-TRU-009, REQ-SEC-004; INV-12, INV-03, BD-05 (bycorrelation_test.go, routescopes_db_test.go, routehandoff_db_test.go, routescopes_boundary_test.go, routescope_args_test.go, route_request_test.go amended).
- Regression cases added: none. Debt entries: none (see "Safer readings").
- Conflicts raised: none.

### Safer readings taken (A332)
- "Newest by database clock": `context_snapshot` holds one time, `sealed_at`, which the sealer writes from its own clock; there is no database-defaulted column. The reader orders by `sealed_at DESC, snapshot_id DESC`. A database-clock column would need a migration (outside this allowlist).
- Several investigations for one correlation: any more than one is `ErrAmbiguousInvestigation`, even when their principals agree.
- `EvidenceUnavailable` is false whenever a sealed snapshot exists (the table records no such flag).
- The typed data is one field, `Session intake.Session` (principal and role from the investigation, tenant from the request).
- The handoff replaces any `Session` already on the request; it is the only writer.

## Pre-work for the next agent
- Exact next step: EL pushes the green, reads the run. The composition (cmd/core, not in this change) builds `RouteDecider{Scopes: RouteScopeReader}` and wraps it in `RouteSessionHandoff{Sessions: investigations.Store (orchestrator_rw), Next: decider}`; the worker's `RouteDecision` conversion calls the handoff.
- Open questions (owner): where the ESCALATING handoff supplies CreateGrantID (EL, after noema#945); whether a database-clock column on context_snapshot is wanted (Architect).
- Files to read first: services/core/orchestrator/portadapters/trust/routescopes.go, routehandoff.go, services/core/orchestrator/investigations/bycorrelation.go.
- Do not touch: noema#945's files.
