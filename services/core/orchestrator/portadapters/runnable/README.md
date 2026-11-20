# runnable

Date: 2026-11-19 · Status: current

Decides which steps of an action graph may run now (A363, interim, relayed, not yet committed). Implements REQ-AG-001 for the Trust adapter; SoT INV-03. Pure: no I/O, no clock, no model. Predicates are evaluated only by `internal/predicate`.

`Runnable(Graph, Facts) ([]string, error)` returns the sorted capability ids whose preconditions and incoming edge conditions are true on the facts and whose dependencies are themselves runnable. Anything not evaluable is not runnable and is listed in `*Errors`. A fact the snapshot lacks is unknown, never true.

Run its tests: `cd services/core && go test -race ./orchestrator/portadapters/runnable`.
