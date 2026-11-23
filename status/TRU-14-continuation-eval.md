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

# Session 2026-11-21 17:00 WAT · TRU-14 (C1, ruling A504 on noema#785, at the Founder's order)

## Session summary
- What changed (PR links): noema#785. `trustport.RouteRequest` keeps only `TenantID`, `CorrelationID`, `CapabilityID` and the two parameters of the route step, `CaseID` and `OwnerQueue`; `VersionToken` and `CreateReceiptID` are removed (A504, interim, `DEC-2026-11-21-architect-rulings-a502-a506`). The Security Reviewer's P2 on this PR is answered by (a): both fields are inside the route step's parameter hash.
- Why: a grant binds one capability, one target, one parameter hash and the plan step. The pack's graph gives the route step exactly two parameters, `case_id` (a fact) and `owner_queue` (a policy param); the capability's input schema is closed over them. `VersionToken` is read fresh by Execution at dispatch, and `CreateReceiptID` is not needed because the case ID comes from the verified create outcome.
- Risks, and how they are contained: the worker's `RouteDecision` in `cmd/core` (wire_worker_ports.go, on main) still has the two fields and its doc says it matches this type field for field; the composition that converts one into the other (EL's) must drop them there in the same change, and `workflows.RouteGrantRequest` keeps them for the worker's own read-back.
- Tests added (REQ-EXE-006, REQ-TRU-002; INV-03, AGENT-08): `route_request_test.go`: the request's exact field set; the pack's route step has exactly the two parameters and its schema is a closed object of them; the worker's queue is not a fact and the case is the only fact. The `routeReq()` literal of `routedecider_test.go` no longer names the removed fields (no assertion changed). Two hand mutants killed and restored (the queue sent as a fact; a field added back).
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL drops `VersionToken` and `CreateReceiptID` from `cmd/core`'s `RouteDecision` in the composition; if any other route parameter is ever needed it goes into the pack graph (inside the hash) with a test.
- Open questions (owner): which evidence snapshot a late continuation uses (C2's note a); `case_id` is still the worker's claim, taken from the verified create outcome (C2's note b); the `Policies`, `RouteScopes` and `Runnable` adapters (EL).
- Files to read first: `orchestrator/portadapters/trust/routedecider.go`, `route_request_test.go`, `packs/fintech/graph/g2_escalate.json`.
- Do not touch: T15 and the Recorder contract.
