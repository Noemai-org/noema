# Session 2026-11-23 05:25 WAT · TRU-14 (C1, ruling A511: the first evaluation fills the plan request from Trust's own steps)

## Session summary
- What changed (PR links): `trust/intake`: `firstCapabilities` (gates_t13_t15.go) and an optional `Service.Runnable`; the tests below were pushed red first (`19c71098`, with the field on `Ports`). **Moved to `Service` in the GREEN commit:** the composition test `TestEveryPortOfTheIntakeServiceIsSet` makes every field of `Ports` a required adapter in `cmd/core` (EL's lane), so an optional port there would have forced a composition change outside this card; a field of the service, like a continuation's own `Runnable`, does not. Two tests of the RED push were wrong and are fixed in GREEN, stated: the read test did not give the fake Recorder a grant-free result, and the nil-port case put a typed nil in the interface.
- Why: A511 (interim, `DEC-2026-11-21-architect-rulings-a507-a517`): `intake.PlanRequest` carries Runnable (its `Capabilities`), `PolicyParams`, `Facts` and `EvidencePacket`, filled from Trust's own steps. noema#785 and TRU-14 already added the fields and the continuation fills them; the first evaluation filled only `PolicyParams` (T11) and sent no capabilities.
- The ambiguity, written down (AGENT-02): an empty `Capabilities` means "plan the first steps as before", so an authorized-and-runnable set that is empty would read as "all". **Smallest safe default (this PR): the first evaluation of a write asks the `Runnable` port once, after T11, and sends authorized ∩ runnable, sorted, without repeats. An empty intersection is UNKNOWN PLAN_INVALID at T14 and the Planner is never asked; a port error or panic is a dependency failure, never a pass. A read does not ask. No port keeps the earlier request.** What would invalidate it: the Architect ruling that an empty set means "no step is runnable now, wait" (a WAIT, not PLAN_INVALID), or that the first evaluation must not filter by runnable at all.
- Not decided here (no ruling names it): `Facts` and `EvidencePacket` stay empty on a first evaluation (a first evaluation has no verified result to carry); whether the sealed snapshot ID is the "sealed evidence packet reference" of `plangraphs.PlanContext` is a question for the Architect.
- Tests added (REQ-TRU-004, REQ-PLN-002; INV-03, INV-02): `trust/intake/first_evaluation_runnable_test.go`.

## Pre-work for the next agent
- Exact next step: the composition (EL) sets `Service.Runnable` to the `runnable` adapter where it builds `plangraphs.PlanContexts` (until then the first evaluation sends no capabilities, as before).
- Files to read first: `trust/intake/ports_continuation.go`, `orchestrator/portadapters/plangraphs/types.go`.
- Do not touch: `packs/fintech/graph/*.json` (A510).

# Session 2026-11-23 06:55 WAT · TRU-14 (C1, ruling A531: nothing runnable is its own result)

## Session summary
- What changed (PR links): noema#896. A531 (interim, relayed by the Founder, not yet committed to `docs`): an empty runnable set is neither PLAN_INVALID nor WAIT. T12 (`afterClocks`, `noRunnable`, `trust/intake/steps_t10_t12.go`) ends a first evaluation of a write with ESCALATE `NO_RUNNABLE_CAPABILITY` (all three layers passed) when the pack routes that reason to a handoff, and with UNKNOWN when no handoff is routed, when `Service.Handoffs` is not set, or when it fails or panics. Never a grant. The T14 PLAN_INVALID of the earlier head stays as the backstop. The ops alert is the orchestrator's, raised on the reason: Trust has no alert port.
- Contract: `CoreReasonCode` gains `NO_RUNNABLE_CAPABILITY` (additive minor in `enums.v1.json`, 20 to 21; migration note in `contracts/README.md`; Go and Pydantic regenerated; `state-registry.md`; a valid fixture; the count pin in `decide/reason_enum_test.go` moves to 21 with the name asserted). `/contracts` needs both Founders (CODEOWNERS).
- Choices the Founder made for the three open points (asked and answered 23 Nov): a new core reason code; the handoff is a pack route lookup (`Service.Handoffs`, `Routed`); the orchestrator raises the alert.
- Tests added (REQ-TRU-004, REQ-RCV-003; INV-03, INV-16): `trust/intake/no_runnable_capability_test.go` (handoff routed, two empty shapes; no handoff, failing, panicking and no port are UNKNOWN; a runnable capability and a read ask nothing; the reason is a core code). Mutants killed: the routed check and the stop flag; one survived by design of the harness: dropping the nil-port check still fails closed because the nil call panics and the evaluation turns a panic into UNKNOWN, so the typed error is not distinguishable from outside.
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL sets `Service.Runnable` and `Service.Handoffs` in the composition (a pack adapter that answers `Routed` from the pack's routes for the reason `NO_RUNNABLE_CAPABILITY`); the orchestrator raises the ops alert on that reason; the pack (C2) declares the handoff route.
- Files to read first: `trust/intake/steps_t10_t12.go` (`noRunnable`), `trust/intake/ports_continuation.go` (`Handoffs`).
- Do not touch: `packs/fintech/graph/*.json` (A510).
