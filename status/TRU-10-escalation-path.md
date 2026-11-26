# Session 2026-11-24 05:15 WAT · TRU-10 (escalation path: by-correlation read, start site, DispositionDecisionID pass-through)

State: GREEN held locally, not pushed. DB tests run in CI only (pgsodium is missing locally).

## Session summary
- What changed (PR links): branch feat/TRU-10-escalation-path2, no PR yet. (1) `trustport.Dispositions` (consumer-side, by correlation, typed `ErrNoDisposition`) and its read-only Postgres reader `trustpg.Dispositions` (tenant's trust_rw login, forced RLS, two ESCALATE rows are `ErrBadAnswer`); (2) the single `DispositionDecisionID` field on `workflows.EscalateInput` and `workflows.RouteGrantRequest`, passed through by the workflow and by the route issuer into `RouteDecision`; (3) `RouteDecider.DecideRouteAfter(ctx, req, disposition)`, which forwards it into `ContinuationRequest.DispositionDecisionID` and re-reads it from the Record by correlation when none is given and a `Dispositions` port is set (A539; the intake's A562 rule is not repeated); (4) `cmd/core/worker_start.go`: `EscalationStarter.Start`, the start site of `EscalateCase` (verified session's tenant, deterministic workflow id per tenant and correlation, idempotent through the SDK).
- Why: A503, A534, A539, A562, A615, A623 (interim, relayed, not yet committed).
- Risks, and how they are contained: see the PR body (safer readings, uncovered points).
- Tests added (REQ-TRU-010, REQ-TRU-002, REQ-EXE-006; INV-03, INV-12, INV-30): `workflows/escalate_disposition_test.go`, `trust/routedecider_disposition_test.go`, `trustpg/dispositions_test.go`, `cmd/core/wire_worker_disposition_test.go`, `cmd/core/worker_start_test.go`.
- Regression cases added: none (no bug).
- Debt entries: none.
- Conflicts raised: none. RouteRequest keeps its A504 guard: the disposition is an argument, not a field.

## Pre-work for the next agent
- Exact next step: EL reads the red job, pushes the green commit. Then the composition (EL's main.go): build `trustpg.NewDispositions`, set `RouteDecider.Dispositions`, convert `RouteDecision` into `RouteRequest` plus the disposition and call `DecideRouteAfter`, build `NewEscalationStarter` over the Temporal client, and call `Start` where an ESCALATING investigation hands off.
- Open questions (owner): the RouteScopes production reader is not built (see the PR body); who decides the create step and so yields `CreateGrantID`; the call of `Start` from the orchestrator (Architect/EL).
- Files to read first: `cmd/core/worker_start.go`, `orchestrator/portadapters/trust/routedecider.go`, `orchestrator/portadapters/trustpg/dispositions.go`.
- Do not touch: `services/core/migrations/`, `contracts/`, `route_request_test.go` (A504).
