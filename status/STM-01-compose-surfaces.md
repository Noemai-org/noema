# STM-01 · final composition, lane 4: the surfaces

State: red pushed; green held locally for the Engineering Lead (not pushed, no PR opened). Not Done.

# Session 2026-11-23 · STM-01

## Session summary
- What changed (PR links): none opened. Branch `feat/STM-01-compose-surfaces`. New files in `services/core/cmd/core/`: `compose_surfaces.go` (`SurfaceSeams`, `Surfaces`, `buildSurfaces`, `withSurfaceReadiness`), `compose_surfaces_journal.go`, `_events.go`, `_runner.go`, `_gateway.go`, `_recovery.go`, `_serve.go` and their tests. Edits to existing files: `compose.go` (field `Components.Events`, the `events route` line of `missing()`, the events route in `apiRoutes`, the `Surfaces` seam of `composeDeps`, the `newProcess` call), `main.go` (the process serves through `serve` and exits non-zero on a fatal error), `compose_test.go` and `wire_readiness_test.go` (the new component's name).
- Why: A537 (interim, relayed): `/readyz` names components that are not built; the surfaces are built from the merged adapters and every part that cannot be built is a named gap (A469, A486).
- Built: the Journal over the per-tenant Record pools (`tenantJournal`); the events route (A469/A486: absent setting is a gap line, invalid is fatal); the write runner and the Input Gateway with its workers over it; recovery (settings, reader seam, `NoDispatch` runner, one startup pass per served tenant, Report counts logged, `/readyz` not ready until the pass ends); the explainer port from a seam; `serve` with the worker and recovery `Fatal()` select and a non-zero exit.
- Named gaps: the write path (Executor, Verifier, Bindings come only from `SurfaceSeams.Write`; none is built in production, so the gateway and recovery are gaps); the recovery Postgres reader (`recovery.Reader`, not on main); the explainer (its facts source has no implementation on main and its templates need the pack accessor of lane 1); the worker's activities (the Execution Controller's ledger, dispatcher and receipt signer have no composition: nobody's lane).
- Risks, and how they are contained: the startup pass bound is derived (tenants x batch x run timeout), not a number of its own; the pass runs in the background so the health routes answer, and `/readyz` waits for it (a safer reading, A476). A pass that cannot list its tenants is fatal.
- Tests added (REQ-STM-001, REQ-API-001, REQ-INP-002, REQ-VER-004, REQ-EXE-006; INV-03, INV-12, INV-30, AGENT-16): one test file per component, named above.
- Regression cases added: none beyond the above. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: when the Execution Controller composition and the Outcome request builder land, set `SurfaceSeams.Write` in `productionDeps()`; when the recovery reader lands, set `SurfaceSeams.Recovery`; when the facts source and the pack templates land, set `SurfaceSeams.Explainer`.
- Open questions (EL): see the PR body, "Safer readings taken".
- Files to read first: `services/core/cmd/core/compose_surfaces.go`, `compose.go` (`newProcess`).
- Do not touch: `wire_understanding_compose.go` (EL, #900), `trust/**`, `migrations/`, `contracts/`.
