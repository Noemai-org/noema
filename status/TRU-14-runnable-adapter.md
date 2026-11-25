# Session 2026-11-25 · TRU-14 (C1, A684: the Runnable adapter over the pack's action graph)

## Task card
- Task: TRU-14 (A511: the first evaluation of a write asks `intake.Runnable` which authorized capabilities may run now; A531: nothing runnable is its own result). A684 lists `Service.Runnable` ("an intake.Runnable adapter over the pack's graph conditions") among the named gaps of the G1 critical path.
- PERD REQ: REQ-AG-001, REQ-TRU-004. SoT: INV-01, INV-03, INV-14, INV-16, AGENT-16; A363, A511, A531, A551, A684 (interim, relayed).
- Allowlist: `services/core/orchestrator/portadapters/runnableport/` (new), `services/core/orchestrator/portadapters/trustpack/` (the graph accessor), this note. No migration, no contract change, no new dependency.
- Plan: tests first (a stub that answers nothing). `trustpack.Pack.ActionGraphData(graphID)` gives a graph's roots, nodes (kind, capability, preconditions) and edges (condition) as a copy. `runnableport.PackGraphs` turns it into `runnable.Graph` for an intent node through `GraphID(node)`, a function the composition sets (from its configuration today, from the node's `action_graph` after noema#963). `runnableport.Adapter` joins `Graphs` and a `FactsSource` seam to the pure `runnable.Runnable`.
- The two choices, written down (AGENT-02): (1) A step the facts cannot evaluate is not runnable, and is not an error when other steps are: the escalation chain's later steps read the case the first step creates, so on a first evaluation they are unevaluable by design. (2) An empty answer that comes with unevaluable steps is an error (`ErrNotEvaluable`, so the intake ends UNKNOWN), never "none"; an empty answer with no unevaluable step is the question's own answer, "nothing runnable now" (A531). Smallest safe default; what would invalidate it: a ruling that an unevaluable precondition should read as ESCALATE.
- Faults: a missing seam, a facts source that fails or returns nil, a node with no graph, a graph the pack lacks, a query missing a part, a cancelled context, facts that declare none of the variables.
- Record events: none (an adapter over pack data and facts).

## Session summary
- What changed (PR links): see the PR body. Red pushed alone.
- Why: the composition has no `Runnable`, so a first evaluation of a write sends no capabilities to the planner and `/readyz` names `Service.Runnable`.
- Tests added (REQ-AG-001, REQ-TRU-004; INV-03, INV-14, INV-16): `runnableport/runnableport_test.go`, `scan_test.go`; `trustpack/graphdata_test.go`.

## Pre-work for the next agent
- Exact next step: a production `FactsSource` (see the [OPEN] in the PR body) and EL sets `Service.Runnable` from `runnableport.Adapter{Graphs: PackGraphs{...}, Facts: ...}`.
- Files to read first: `trust/intake/ports_continuation.go`, `portadapters/runnable/runnable.go`, `portadapters/trustpack/graphdata.go`.
- Do not touch: `packs/fintech/graph/*.json` (A510).
