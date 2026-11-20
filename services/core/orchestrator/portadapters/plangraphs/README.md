# plangraphs

Date: 2026-11-19 · Status: built, not yet composed

Implements `trustport.PlanGraphs` (noema#751) for the Planner adapter. **Implements:** REQ-TRU-009; INV-02, INV-18, AGENT-09.

It reads the pack's action graph for an intent node through the `Pack` port (pack data, never imported as code, SoT §51) and the admitted intent's slots through the `Intents` port, and plans only the RUNNABLE steps (`PlanContext.Runnable`; the first evaluation of G1 plans only create_escalation_case). Params have six kinds, the contract of noema#765 (the Founder's text of 19 Nov 2026, interim, relayed, not yet committed): `slot` (a RESOLVED slot), `const` (a pack literal), `map` (the resolved slot's value looked up in the pack's map; a value not in it is refused, never passed through), `fact` (a named run-time fact of the `PlanContext`), `policy_param` (the winning PERMIT rule's param for the step's capability) and `evidence_packet` (the sealed packet reference, opaque). An absent fact, policy param or packet is `ErrNotPlannable`; any other kind (DEFAULT is gone), an unresolved slot, a float, a target that is not the stored intent's, or a graph or intent that is not the requested one is a refusal. Every refusal wraps `trustport.ErrPlanGraphInvalid`, which the Planner maps to `intake.ErrPlanInvalid`. Every step's `TargetRef` is the stored intent's target. Returned values are copies.

`Graph` (the `trustport.PlanGraphs` method) takes the `PlanContext` from the `PlanContexts` port; `GraphWith` takes it as an argument. Mapping from the intake's PlanRequest (a parallel lane): `Runnable` from `runnable.Runnable` (noema#782), `PolicyParams`, `Facts` and `EvidencePacket` from the same-named fields.

**Open:** the `Pack` implementation of the composition fills `ParamSpec` from noema#765's graph-node `params`. 

Tests: `go test -race ./services/core/orchestrator/portadapters/plangraphs/`.

**A440, A454 and SR conditions (interim, relayed, not yet committed):** the node's slot declaration (`PackGraph.Slots`, `SlotDecl{Kind, ResourceKind}`) is the single authority of a slot's kind (Resource, Quantity, Party, TimeRef, Reference; Enum is not a slot type; `ResourceKind` for Resource only). A slot or map param's `SlotKind` and `ResourceKind` only declare the same and must EQUAL the slot's (defence in depth after packcheck); the resolved value carries its own `Kind` and `ResourceKind`, which must equal the slot's. A slot with no valid declaration, or any difference, is `ErrSlotKind`, refused at planning and named. Every string from a slot (nested too) has no digit run over four and at most `Adapter.MaxTextBytes` bytes (`ErrFreeText`); the bound has no default and zero is `ErrConfig`.
