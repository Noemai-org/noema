# Status · TRU-06 (clocks and SLA evaluation)

## Session 2026-11-04 · slice 1: EVALUATE_SLA_STATUS
## Session summary
- What changed: `services/core/trust/clocks/clocks.go` (new), `clocks_test.go` (new), package doc. `Evaluate(clock, anchor, now, atRisk)` returns started_at, deadline_at, elapsed, remaining and the phase (NOT_STARTED, RUNNING, AT_RISK, BREACHED) of PERD §23.5.
- Why: TRU-06 (REQ-TRU-006, P1); Needs TRU-03 (merged, noema#121) and PLT-07 (Done). Started because every G1 item I own was blocked (TRU-05 on REC-01), so it delays no G1 item.
- Risks, and how they are contained: (1) The at-risk fraction (25%, PERD §23.5 [REC]) is not held in this package: it is an input (`Ratio`), because decision code holds no threshold literal (REQ-OPS-003) and I do not set thresholds. The caller reads it from configuration; wiring and the config key arrive with the decision path. (2) Inputs that cannot be evaluated (an anchor after now, a non-positive duration, an unknown label, a bad fraction, an overflow) are `ErrInvalid`, which the caller must turn into UNKNOWN (INV-16).
- Tests added (REQ-TRU-006, INV-21): missing anchor is NOT_STARTED with nothing assumed; phase boundaries (exactly 25% remaining is RUNNING, one second less is AT_RISK, the deadline is BREACHED); the label is carried and an unknown label is refused; seven kinds of invalid input; a seeded property (5,000 anchors × 24 instants): the phase never moves backwards, remaining never grows, elapsed + remaining = duration before the deadline. Seen failing first (did not compile). Mutation: 4 of 4 mutants caught (< to <=, <= to <, the future-anchor check, elapsed).
- Regression cases added: none (no bug fixed).
- Debt entries: none. Conflicts raised: none.

## Not built (named, not hidden)
- The routing priority derived from the phase (pack routing rules; priority raised, never lowered; the "no downgrade" acceptance) needs pack data and the decision path.
- The `SLA_EVALUATED` Record event (all fields) needs TRU-05.
- EC-T05 and the Temporal time-skip test (TRU-06's "Done when") need the decision path and the workflow; they are not claimed here. TRU-06 is therefore not Done.

## Pre-work for the next agent
- Exact next step: after TRU-05, call `Evaluate` from the T12 step with the anchor read from evidence, record `SLA_EVALUATED`, map the phase to priority through the pack's routing rules.
- Do not touch: `noema#94`, `noema#90`.

# Session 2026-11-25 12:00 WAT · TRU-06 · slice 2: the priority routing and SLA_EVALUATED (A632, A640, A666)

## Task card (PERD §61.4)
- Task: TRU-06 (REQ-TRU-006, P1). SoT: INV-01, INV-14, INV-16, INV-21, AGENT-16, AGENT-25. Rulings (interim, relayed, not yet committed): A632, A640, A644, A666.
- Allowlist: `services/core/trust/clocks/` (new `routing.go` and its test), `services/core/trust/recorder/` (new `sla_event.go` and its test, the package doc), this file.
- Acceptance: (1) routing priority derives from the phase through a consumer-side `PriorityLookup`, raised and never lowered; a phase with no entry is NORMAL with no queue; an empty queue is no re-route. (2) `SLA_EVALUATED` is appended once per clock evaluation in the caller's transaction, by the emitter's identity, with every field as its payload. (3) No clock read, no number or name in code.
- Tests first: `trust/clocks/routing_test.go` (examples, a rapid property, a source scan) and `trust/recorder/sla_event_test.go` (real database and hash chain).
- Faults exercised: a failed Record append (the write fails and nothing remains), a rolled-back transaction, a request naming another tenant, an invalid request.
- Record events asserted: `SLA_EVALUATED` with `written_by` and `actor` core-trust and `object_ref` the correlation.
