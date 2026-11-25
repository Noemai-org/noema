# Session 2026-11-25 · TRU-14 (C1, A694: RunnableQuery gains the optional EvidencePacket)

## Session summary
- What changed (PR links): RED: the field `RunnableQuery.EvidencePacket` is declared and never filled; three tests (`runnable_query_packet_test.go`). Green follows.
- Why: A694 (interim, relayed; docs DEC-2026-11-25-architect-rulings-a690-a700): EL builds the production FactsSource (noema#987 names it open) and the graphs' predicates read `evidence_packet_hash`; `PlanRequest.EvidencePacket` has it and `RunnableQuery` did not.
- Risks, and how they are contained: an intake Go type only; no contract, migration or dependency. The field is optional and has one source (the continuation's own packet, the same value the plan request carries for the step); a first evaluation leaves it empty. No guard pins RunnableQuery's fields (the TRU-12 guard pins `intake.Request`).
- Tests added (REQ-TRU-004, REQ-AG-001; INV-03, INV-16): `runnable_query_packet_test.go` (three).
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL builds the FactsSource over the sealed snapshot, the case-list reader and this field (noema#987).
- Files to read first: `trust/intake/ports_continuation.go`, `steps_t10_t12.go` (`stepSet`).
- Do not touch: the TRU-12 guard member set of `intake.Request`.
