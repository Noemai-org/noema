# Status · PLN-01 (Core Engineer 1) · deterministic Planner and canonical plan hash

Updated 2026-11-03. Plan task PLN-01: owner C1+AG; REQ-PLN-001, REQ-PLN-002; Needs EXE-01; done when "Property tests pass".

## State: in review (draft until checks are green at the final head and Codex has reviewed). Slice 1 of 2.

**Stacked on noema#131 (EXE-01).** The planner needs the registry. On the Architect's instruction of 3 Nov, this branch is cut from #131's head and the PR targets `main`; it shows #131's commits until #131 merges, and I merge `main` in then. PLN-01 cannot merge before #131.

## Session 2026-11-03 · PLN-01

### Session summary
- What changed (PR links): the PLN-01 pull request on `feat/PLN-01-planner`.
- Why: INV-18 (a plan with an unregistered or prohibited ID cannot be hashed); G1 path.
- Risks, and how they are contained: slice 1 chooses no steps, so the Planner's determinism over the graph is not yet shown; the REQ-PLN-001 acceptance ("random graphs") waits for slice 2. The hash is checked against a second route (decode, then canonicalise), and every field of the document is covered by a property test.
- Tests added (REQ-PLN-001, REQ-PLN-002, INV-18): `planner_test.go` (5 tests), `property_test.go` (4 `rapid` properties).
- Regression cases added: none (new code). Mutations run and reverted: registry skipped in `New`; registry skipped in `HashDocument`; steps not numbered; params dropped from the hash; postconditions dropped. One mutation (floats accepted by the type switch) survives by design: the hash step also refuses floats.
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: slice 2, the graph walk (`plan(intent_node, disposition, classification, clock_status, graph_version)`), `PLAN_PRECONDITION_UNMET`, and the "random graphs" property, after PCK-04 settles the Action Graph contract.
- Open questions (owner): whether Trust or the Planner writes `PLAN_CREATED` (Engineering Lead).
- Files to read first: `services/core/execution/planner/README.md`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).
