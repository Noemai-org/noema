# Session 2026-11-23 05:25 WAT · TRU-14 (C1, ruling A511: the first evaluation fills the plan request from Trust's own steps)

## Session summary
- What changed (PR links): scaffold and RED only so far (the PR is Draft): an optional `Ports.Runnable` and the tests below.
- Why: A511 (interim, `DEC-2026-11-21-architect-rulings-a507-a517`): `intake.PlanRequest` carries Runnable (its `Capabilities`), `PolicyParams`, `Facts` and `EvidencePacket`, filled from Trust's own steps. noema#785 and TRU-14 already added the fields and the continuation fills them; the first evaluation filled only `PolicyParams` (T11) and sent no capabilities.
- The ambiguity, written down (AGENT-02): an empty `Capabilities` means "plan the first steps as before", so an authorized-and-runnable set that is empty would read as "all". **Smallest safe default (this PR): the first evaluation of a write asks the `Runnable` port once, after T11, and sends authorized ∩ runnable, sorted, without repeats. An empty intersection is UNKNOWN PLAN_INVALID at T14 and the Planner is never asked; a port error or panic is a dependency failure, never a pass. A read does not ask. No port keeps the earlier request.** What would invalidate it: the Architect ruling that an empty set means "no step is runnable now, wait" (a WAIT, not PLAN_INVALID), or that the first evaluation must not filter by runnable at all.
- Not decided here (no ruling names it): `Facts` and `EvidencePacket` stay empty on a first evaluation (a first evaluation has no verified result to carry); whether the sealed snapshot ID is the "sealed evidence packet reference" of `plangraphs.PlanContext` is a question for the Architect.
- Tests added (REQ-TRU-004, REQ-PLN-002; INV-03, INV-02): `trust/intake/first_evaluation_runnable_test.go`.

## Pre-work for the next agent
- Exact next step: GREEN in `trust/intake/gates_t13_t15.go` (`planRequest`, `validPlan`); the composition (EL) sets `Ports.Runnable` to the `runnable` adapter and builds `plangraphs.PlanContexts`.
- Files to read first: `trust/intake/ports_continuation.go`, `orchestrator/portadapters/plangraphs/types.go`.
- Do not touch: `packs/fintech/graph/*.json` (A510).
