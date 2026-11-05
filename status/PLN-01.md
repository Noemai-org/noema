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

## Session 2026-11-03 (later) · PLN-01 · Codex review 5757658878
- **Fixed:** `Plan.Document()` returned a shallow copy, so a caller could change the plan and its hash through nested maps, param bytes and predicate lists. It is now a deep copy; `TestMutatingTheReturnedDocumentCannotChangeThePlan` (fails before the change, passes after) mutates every nested collection, and `TestMutatingTheInputSpecsAfterBuildingCannotChangeThePlan` shows the input is copied on ingestion too.
- **Stack:** this branch now includes noema#131's fix commit (`fea9ebd`: a regression test that an unknown member at any level fails the pack load; Codex's finding on #131 did not reproduce because the generated pack types are closed).

## Session 2026-11-05 · PLN-01 slice 2 (graph walk), Engineering Lead
- What changed: `execution/planner/walk.go` and `walk_test.go`. `planner.Walk(reg, graph, intentNode, params)` turns an Action Graph v2 into a plan: one step per node, a node only after every node it depends on, ties by node ID, so the plan (and its hash) does not depend on how the document orders nodes and edges. It refuses, and builds nothing for: no nodes, a duplicate node, an edge naming an unknown node, a self edge, a cycle, a root with an incoming edge, a conditional edge, and any capability the registry refuses (unregistered or prohibited, INV-18).
- **Stacked on noema#104** (the generated `ActionGraphV2` type and the fintech graph postconditions are there). It cannot merge before #104; main is merged in after.
- Tests (REQ-PLN-001, REQ-AG-001, INV-18; written first, the first commit does not compile): the three fintech graphs each walk to a plan that follows every edge and hashes the same twice; node conditions are copied into steps; 11 refusals; a prohibited capability gives CAPABILITY_PROHIBITED; a `rapid` property over random acyclic graphs (1 to 12 nodes, listed in a random order) checks that the plan holds every node once, follows every edge, and has the same hash however the document is ordered.
- Mutation: 8 mutants. Six caught at once; two survived because another check masked them (the cycle check behind the root check; the cycle test emptied the plan), so the tests now isolate them (a cycle apart from plannable nodes; a root with an incoming edge and no cycle) and both are caught. Two mutants are equivalent and remain: no self-edge check (the cycle check refuses it too) and taking the last ready node instead of the first (still a valid, deterministic order).
- Not built: choosing the graph from disposition, classification and clock status; conditional edges; `PLAN_PRECONDITION_UNMET`; who writes `PLAN_CREATED` (open question to the Engineering Lead).
- Do not touch: noema#94, noema#90.
- **Codex P2 on noema#180, fixed:** the walk started from every zero-dependency node and only checked the roots that were listed, so a graph with omitted or wrong `roots` could plan from an undeclared entry point. The declared roots must now be exactly the nodes that depend on nothing (a root that is no node, a root with an incoming edge, an undeclared zero-dependency node and a graph with no roots are all refused). Tests first (three new refusals, seen failing); the cycle and self-edge cases no longer clear `roots`, so another check cannot mask them. Mutants of the three root checks and of the cycle check are caught; the `no roots` guard is an equivalent mutant (an all-cycle graph is refused by the cycle check and by `New` anyway).
