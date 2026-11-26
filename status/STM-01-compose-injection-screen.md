# Status · STM-01 · compose injection screen inputs (A704)

State: red and green committed locally; not pushed.

# Session 2026-11-26 · STM-01

## Session summary
- What changed (PR links): none yet. `cmd/core/compose_injection.go` (new): the five required `NOEMA_INJECTION_*` settings, `newInjectionScreen`, `buildInjectionScreen`, the Alert, `correlationOfContext`, the failure counter. `compose.go`: one call, a `Components.InjectionFailures` field, the screen handed to `trustWiringIn`. `wire_readiness_test.go`: the new field is listed as not a component. `deploy/compose/bench-stack.yml`: the five ruling values.
- Why: A704 (the inputs of the injection screen), A330 and AGENT-16 (no default in code).
- Risks, and how they are contained: no Record port is wired, so in production every finding is a Record failure that raises the Alert and the counter (never a silent pass); a payload is still admitted (advisory, SoT 29). An invalid setting stops the process; an absent one is a named gap.
- Tests added (REQ-STM-001, REQ-CTX-007, REQ-AUD-001; INV-16, INV-19; AGENT-13, AGENT-16): `compose_injection_test.go`. 19 hand mutants, all killed.
- Regression cases added: the same tests.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: wire the `injection.Recorder` (`RecordRecorder`, `core-evidence` through the tenant's evidence login, A536) and pass it at the one call in `compose.go`; then the Evidence, Policies, Reconciler and Recorder lanes, in order.
- Open questions (owner): where `noema_injection_record_failures_total` is exported (EL: no metric registry exists on main).
- Files to read first: `cmd/core/compose_injection.go`.
- Do not touch: `evidence/injection`, `evidence/payloadread`, `platform/telemetry`.
