# plangraphs

Date: 2026-11-19 · Status: built, not yet composed

Implements `trustport.PlanGraphs` (noema#751) for the Planner adapter. **Implements:** REQ-TRU-009; INV-02, INV-18, AGENT-09.

It reads the pack's action graph for an intent node through the `Pack` port (pack data, never imported as code, SoT §51) and the admitted intent's slots through the `Intents` port. Each step's params are built only from RESOLVED slots and values the pack declares (`SLOT`, `CONST`, `DEFAULT`); a param with any other source, or a slot that is absent or unresolved, is a refusal (`ErrParamSource`, `ErrUnresolvedSlot`), and so is a step target that is not the stored intent's (`ErrTargetMismatch`) or a graph or stored intent that is not the requested one (`ErrWrongIntent`). Each wraps `trustport.ErrPlanGraphInvalid`, which the Planner maps to `intake.ErrPlanInvalid`. Every step's `TargetRef` is the stored intent's target. Returned values are copies.

**Open:** the pack schema (`action-graph.v2`) has no param declaration, so the composition's `Pack` cannot yet fill `ParamSpec` from the pack on disk. A contract change (AGENT-15) is needed; it is outside this PR.

Tests: `go test -race ./services/core/orchestrator/portadapters/plangraphs/`.
