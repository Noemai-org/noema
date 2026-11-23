# STM-01 Understanding half in Compose

State: In review (green commit local until EL pushes it).

# Session 2026-11-23 STM-01

## Session summary
- What changed: `buildComponentsWith` calls `composeUnderstanding` (new `wire_understanding_compose.go`), which calls `buildUnderstanding(env, clk, recorder, admissions)` with the admissions port Compose already builds (`admissionsport.Port{Reader: &admission.Reader{DBs: records}}`), plugs `Ports.Interpreter` and keeps RuleSet, Mapping, PackRef and SealTimeout in `Components.Understanding`. None of the twelve settings given: the plain `interpreter` gap. Some missing: no error, `/readyz` names `interpreter: NAME missing` for each (fixed text, no value). A present invalid value: fatal, named, no value (A469, A486; A506, A512 interim, relayed, not yet committed).
- Why: the pack-source follow-up, Understanding half only.
- Risks: a partial set is not validated beyond its numbers and the model URL until it is complete; the files and the pack are judged when the half is built.
- Tests added (REQ-STM-001, REQ-INT-001, INV-12, AGENT-16): `main_compose_understanding_test.go`.
- Regression cases added: none (new behaviour). Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: Trust and Evidence wiring takes `Components.Understanding` (RuleSet, Mapping, PackRef as PackVersion, SealTimeout); CorrelationOf = the snapshot's own correlation, required equal to the evaluation's (A377). Both stay the named gaps `trust.*` and `evidence`.
- Open questions: action-graph files and Planner (graph-hash-pin ruling and TRU-14 inputs pending); Recorder lifetimes wiring; ownership map.
- Files to read first: services/core/cmd/core/wire_understanding_compose.go, compose.go.
- Do not touch: trust/**, the action graphs.
