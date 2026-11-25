# Session 2026-11-25 · TRU-14 (C1, A684: the Runnable adapter over the pack's action graph)

## Task card
- Task: TRU-14 (A511: the first evaluation of a write asks `intake.Runnable` which authorized capabilities may run now; A531: nothing runnable is its own result). A684 lists `Service.Runnable` ("an intake.Runnable adapter over the pack's graph conditions") among the named gaps of the G1 critical path.
- PERD REQ: REQ-AG-001, REQ-TRU-004. SoT: INV-01, INV-03, INV-14, INV-16, AGENT-16; A363, A511, A531, A551, A684 (interim, relayed).
- Allowlist: `services/core/orchestrator/portadapters/runnableport/` (new), `services/core/orchestrator/portadapters/trustpack/` (the graph accessor), this note. No migration, no contract change, no new dependency.
- Plan: tests first (a stub that answers nothing). `trustpack.Pack.ActionGraphData(graphID)` gives a graph's roots, nodes (kind, capability, preconditions) and edges (condition) as a copy. `runnableport.PackGraphs` turns it into `runnable.Graph` for an intent node through `GraphID(node)`, a function the composition sets (from its configuration today, from the node's `action_graph` after noema#963). `runnableport.Adapter` joins `Graphs` and a `FactsSource` seam to the pure `runnable.Runnable`.
- The two choices, written down (AGENT-02): (1) A step the facts cannot evaluate is not runnable, and is not an error when other steps are: the escalation chain's later steps read the case the first step creates, so on a first evaluation they are unevaluable by design. (2) An empty answer is "none" (no error: the question's own answer, A531) unless the start of the graph could not be evaluated (a root, an error that names no node or an unknown node, a repeated node, a cycle): then it is `ErrNotEvaluable` (the intake ends UNKNOWN), never "none". Refined while writing the green: a rule that any unevaluable step makes an empty answer an error would make every first evaluation of the escalation chain UNKNOWN, because its later steps read the case the first step creates. Smallest safe default; what would invalidate it: a ruling that an unevaluable precondition should read as ESCALATE.
- Faults: a missing seam, a facts source that fails or returns nil, a node with no graph, a graph the pack lacks, a query missing a part, a cancelled context, facts that declare none of the variables.
- Record events: none (an adapter over pack data and facts).

## Session summary
- What changed (PR links): noema#987. Red `40dbd902` pushed alone; the green adds `Pack.ActionGraphData`, `runnableport.Adapter` and `PackGraphs`, a README, and one test (`TestAGraphOfReadsIsRunnableWholeAndWithoutAnError`) that was written after the first green run because a hand mutant (the success answer dropped) survived. One red expectation was corrected in the green: `expectedGraph` compared a nil precondition list with the accessor's empty list; the premise changed, not the assertion.
- Why: the composition has no `Runnable`, so a first evaluation of a write sends no capabilities to the planner and `/readyz` names `Service.Runnable`.
- Tests added (REQ-AG-001, REQ-TRU-004; INV-03, INV-14, INV-16): `runnableport/runnableport_test.go` (12 tests, one rapid property), `scan_test.go`; `trustpack/graphdata_test.go` (3). Hand mutants killed: an unevaluable start read as none; any error with an empty answer read as an error; the snapshot not required; the success answer dropped. One equivalent mutant survives (the explicit nil-facts check: `runnable.Runnable` refuses nil facts the same way).
- Regression cases added: the same tests. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: a production `FactsSource` (see the [OPEN] in the PR body) and EL sets `Service.Runnable` from `runnableport.Adapter{Graphs: PackGraphs{...}, Facts: ...}`.
- Files to read first: `trust/intake/ports_continuation.go`, `portadapters/runnable/runnable.go`, `portadapters/trustpack/graphdata.go`.
- Do not touch: `packs/fintech/graph/*.json` (A510).
