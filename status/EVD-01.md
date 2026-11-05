# Status · EVD-01 (Core Engineer 2) · the FactStatus function

Updated 2026-11-04. Plan task EVD-01 (owner C2+AG; REQ-CTX-001, REQ-CTX-002). Task card: docs#109 (draft, awaiting the Founder).

## Session 2026-11-04 · EVD-01 slice 1

### Session summary
- What changed: `evidence/layer/status.go` (`AssignStatus`, `Read`, `Source`, `ReadClass`, `Authority`, `Absence`) and `status_test.go`. Standard library only.
- Why: REQ-CTX-002, INV-19, INV-23; the Founder's order of 4 Nov 2026 to start EVD-01 slice 1.
- Risks: the function is new and unused; nothing calls it yet. Decisions D1 to D3 (corroborating-only, customer claim, read from the future are UNKNOWN) are the card's defaults, D1 is OPEN for the Architect.
- Tests added: the PERD §21.1 rows, absence never negative (EC-S11, EC-S19), the single exception, totality, determinism and no multi-item status over 50,000 seeded generated reads, and a Go fuzz target. Written first; red against a stub; mutation checks on every rule (the one survivor was a redundant clause, removed).
- Regression cases added: none (new code).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: slice 2, the `evidence_item` and `evidence_payload` tables with the immutability trigger (a migration number after `0007`) and the item builder, with real adapter faults and `EVIDENCE_READ` events.
- Open questions: D1 (Architect); the evidence-source contract has no `absence_semantics` field (slice 1 takes it as an input, no contract change).
- Files to read first: `services/core/evidence/layer/status.go`, PERD §21.1 and §21.6.
- Do not touch: `contracts/` (read-only).
