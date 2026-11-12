# Status · ENV-X5 arm-turn contracts (Core Engineer 2)

## Session 2026-11-10 · arm-turn.v1 and arm-turn-result.v1 (DRAFT; A118 is not yet committed in docs/decisions)

## Session summary
- What changed: `contracts/schemas/arm-turn.v1.json` and `arm-turn-result.v1.json`, 13 + 9 invalid and 3 + 5 valid fixtures (written first, red at 863446c), INVENTORY rows (AGENT-15), regenerated Go and Pydantic models, shape-refusals list regenerated. No tag (A12). No code: the `noema_model.harness.arm_turn` module is not built (it waits for these contracts to be approved).
- Why: Architect sync of 10 Nov 2026, A118: arms A and B use each provider's native tool calling through `providers/`; the forced-schema `{tool, arguments}` route is a labelled diagnostic only; no comparison-arm run before G3. Relay issue noema#364.
- Risks, and how they are contained: the contract is a proposal and may change shape in review; the generated Go types also land in core (the core never uses them); free-form `input_schema` and `arguments` objects are not validated beyond "object" (the tool facade, noema#361, owns the tool input schemas). A native tool-calling adapter in `providers/` may need a new library: that needs an AGENT-14 entry first (A118) and is not started.
- Tests added: the fixtures above (REQ-BEN-007; INV-22 for the no-oracle negative `unknown_field`).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: once A118 is committed and the contracts are approved, build `noema_model.harness.arm_turn` (one turn file in, one result file out, failing tests first) over the existing `providers/` adapters; open an AGENT-14 entry before any new library.
- Open questions (owner): where the tool input schemas live (noema#362, EL); whether the Go types should be excluded from core generation (Architect).
- Files to read first: `contracts/schemas/arm-turn.v1.json`, `services/model/noema_model/harness/`.
- Do not touch: the comparison arms (no run before G3).

## Session 2026-11-11 05:50 UTC · arm-turn.v1 and arm-turn-result.v1, definition of done (A131)

## Session summary
- What changed: A118 is committed (docs/decisions/DEC-2026-11-10-architect-sync-a113-a122.md), so the "Draft until committed" wording in both schema descriptions and the INVENTORY row now cites the record. DoD 3b: the four numbers of the result file (`input_tokens`, `output_tokens`, `cost_micros`, `latency_ms`) now have the same 9007199254740991 maximum that common.v1 uses for PositiveInt; four invalid fixtures were written first (seen accepted locally, not in CI) and the schema change follows. Models regenerated. Main (35d1cd0b) merged in; the patch-id of my own diff is c827fccf02f8585b before and after the merge.
- Why: A131 (definition of done) and A132 (finish the Draft).
- Risks, and how they are contained: a bound is added to a contract that no code consumes yet; nothing reads these files until `arm_turn` is built.
- Tests added: four invalid fixtures (REQ-BEN-007).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: as above; build `noema_model.harness.arm_turn` only after G3 may run a comparison arm and the contracts are approved.
- Do not touch: the comparison arms (no run before G3).
