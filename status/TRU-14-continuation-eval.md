# Status · TRU-14 · the continuation evaluation and the worker's RouteDecider (noema#785)

Updated 2026-11-19. Tier 1 (Trust). Allowlist exception: `services/core/trust/intake/**` (C1's path) at the Founder's order of 19 Nov 2026. Interim, relayed, not yet committed: A335, A363.

## State: red pushed (PR 785, draft); green committed locally, not pushed

# Session 2026-11-19 · TRU-14

## Session summary
- What changed (PR links): noema#785. `Service.EvaluateStep` (one next step, T1 to T15); `PlanRequest` carries `Capabilities`, `PolicyParams`, `Facts`, `EvidencePacket`; `PolicyEvaluation.PermitParams`; `trustport.RouteDecider`.
- Why: A335 gives each escalation step its own decision and grant; the worker's route step needs Trust to decide it after the case step is verified.
- Risks, and how they are contained: only adds refusals. A capability not authorized or not runnable fails closed at T11; the plan of a continuation is exactly one step; T15 unchanged.
- Tests added (REQ-TRU-002, REQ-PLN-002, REQ-EXE-006; INV-03, INV-16): `continuation_test.go`, `continuation_service_test.go`, `routedecider_test.go`.
- Regression cases added: the same.
- Debt entries: none.
- Conflicts raised: none (the continuation runs T1..T15, so PERD 23.3's order is kept).

## Pre-work for the next agent
- Exact next step: the composition needs (a) a `Policies` adapter that fills `PermitParams` from the winning PERMIT rule's `policy.Applied.Params`; (b) a `RouteScopes` over the stored admission; (c) an `intake.Runnable` over the `runnable` package (noema#782); (d) a conversion `trustport.RouteRequest(RouteDecision)` in cmd/core. The planner adapter must read `PlanRequest.Capabilities`, `PolicyParams`, `Facts`, `EvidencePacket` (noema#762 PlanGraphs).
- Open questions (owner): who writes (a) to (c); whether the route step needs the version token as a fact (EL).
- Files to read first: `trust/intake/ports_continuation.go`, `steps_t10_t12.go` (`stepSet`), `orchestrator/portadapters/trust/routedecider.go`.
- Do not touch: T15 and the Recorder contract.
