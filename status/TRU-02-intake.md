# Status · TRU-02 intake (Core Engineer 1) · the Trust intake: T1 to T6

Updated 2026-11-14. The first of four PRs of the Trust assembly (the G1 card of noema#436; rulings A205 and A188). `trust/intake` assembles T1 to T15 behind consumer-defined interfaces; this PR builds the skeleton and T1 to T6. T7 to T15 return `ErrNotBuilt`, so the evaluation ends in UNKNOWN INTERNAL_ERROR at T7 and nothing can be allowed yet.

## State: in review (PR noema#567)

## Session 2026-11-14 · TRU-02 intake

### Session summary
- What changed (PR links): noema#567: `trust/intake/{doc.go,ports.go,intake.go,gates.go,README.md,gates_test.go}`.
- Why: every Trust step exists and has no production caller; the intake is the piece EL's `Trust.Decide` adapter calls.
- Risks, and how they are contained: it adds no allow path and no write path. Every step failure ends in UNKNOWN INTERNAL_ERROR; T1 to T6 each refuse with one fixed outcome and core reason code (`decide.FailAtGate`); T1 passes a session with no tenant or principal to T2 without reading anything, and T2 refuses it.
- Tests added (REQ-TRU-002, REQ-TRU-003, REQ-NFR-002, INV-16, INV-21): `gates_test.go` (16 refusal cases, a read continues with writes disabled, a spy that proves a refusal stops before the later ports, an error and a panic at all eight port methods, configuration and deadline errors, determinism). Red first at `0c5d9e98`.
- Regression cases added: the above.
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: PR 2 (T10 reconciliation, T11 policy, T12 clocks, with the Q1 values as pack data), PR 3 (T7 to T9), PR 4 (T13 to T15, the Decision Record in one transaction, `Trust.Decide` adapter by EL).
- Open questions (owner): where the Q1 pack data lives (A205: at-risk 25% remaining, margin 0.35, floor 0.70, at most 4 options; the margin and the 4 are already in `packs/fintech_v1.json` intent nodes; the clock has no field for the ratio, and `clock.v1.json` is closed: a contract change, both Founders).
- Files to read first: `trust/decide/{evaluate.go,gate.go,basis_builder.go}`, `trust/intake/ports.go`.
- Do not touch: `trust/decide` (TRU-02 is Done there).
