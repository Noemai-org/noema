# TRU-12 · the Planner adapter (T14)

State: red commit pushed (tests and a compiling stub); implementation committed locally, pushed by EL after reading the red job. Branch `feat/TRU-12-planner-adapter`. REQ-TRU-009; INV-03, INV-16, INV-17, INV-18; A329.

# Session 2026-11-19 WAT · TRU-12

## Session summary
- What changed (PR links): `services/core/orchestrator/portadapters/trust/planner.go` and `planner_test.go`: `PlannerAdapter` implements `intake.Planner` over `execution/planner`. The action graph comes as data through the consumer-defined `PlanGraphs`; no pack is imported (INV-14).
- Why: T14 of the intake needs a Planner; the intake imports no execution package.
- Risks, and how they are contained: a refusal of the planner or the registry (CAPABILITY_NOT_REGISTERED, CAPABILITY_PROHIBITED, SCHEMA_INVALID) and `ErrPlanGraphInvalid` map to `intake.ErrPlanInvalid`; every other error is returned wrapped as a dependency failure.
- Tests added (REQ, INV, US or EC IDs): REQ-TRU-009, INV-18 (see `planner_test.go`).
- Regression cases added: none (new code).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the composition (EL's main.go PR) implements `PlanGraphs` from the pack's action graph files and fills `PlannerAdapter{Registry, Graphs}`.
- Open questions (owner): where the step's `TargetRef` comes from (here: the graph source resolves it from the request; plan.v1 carries none) (EL).
- Files to read first: `trust/intake/ports_t13_t15.go`, `execution/planner/planner.go`.
- Do not touch: `trust/**`, `execution/**`.
