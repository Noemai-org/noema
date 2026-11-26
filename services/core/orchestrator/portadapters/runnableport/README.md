# runnableport

Date: 2026-11-25 · Status: current

Implements `intake.Runnable` (A363, A511, A684): which capabilities of the intent node's action graph may run now. It joins two seams the composition fills, `Graphs` (the graph of an intent node; `PackGraphs` reads it from the verified pack) and `FactsSource` (the facts the predicates read, from the sealed snapshot and the caller's facts), to the pure `runnable` package. Implements REQ-AG-001 and REQ-TRU-004; SoT INV-01, INV-03, INV-14, INV-16.

The answer is sorted, free of repeats and never more than the graph holds. A step the facts cannot evaluate is not runnable. When nothing is runnable the answer is "none" (an empty set, no error), unless the start of the graph could not be evaluated or the graph is defective: that is `ErrNotEvaluable`, never "none". A source that fails or gives no facts, a node with no graph (`ErrNoGraph`) and a question missing a part (`ErrQuery`) are errors. The package names no capability, node or graph; a source scan pins it.

Not built: a production `FactsSource` (see the [OPEN] of noema#987). The composition sets `Service.Runnable` from `Adapter{Graphs: PackGraphs{...}, Facts: ...}`.

Run its tests: `cd services/core && go test -race ./orchestrator/portadapters/runnableport`.
