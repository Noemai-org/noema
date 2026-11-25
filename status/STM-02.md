# Status · STM-02 (C1+AG task, taken by IL on the Founder's order of 24 Nov) · pending and unknown never collapse into a terminal state

Updated 2026-11-24. Plan task STM-02: REQ-STM-002; Needs STM-01 (Done); done when "property test over random event orders finds no collapse".

## State: in review (noema pull request for `test/STM-02-no-collapse`). Not Done until merged with a check-run link.

## Session 2026-11-24 · STM-02

### Session summary
- What changed: `services/core/orchestrator/collapse_test.go` only (a test; no production code changed). A rapid property runs random orders of verdicts (zero or one retry of NOT_ACHIEVED or PARTIALLY_ACHIEVED, then one of ACHIEVED, PENDING_EXTERNAL, UNKNOWN, COMPENSATED) and receipts (APPLIED, UNKNOWN) through `RunWrite` and reads the moves back. A second test reads the table: the only arrows from AWAITING_EXTERNAL_OUTCOME, BLOCKED_UNKNOWN and RECOVERING into COMPLETED, REJECTED, ESCALATED or EXPIRED are the two that carry a verified ACHIEVED or a handoff.
- Why: REQ-STM-002 and BD-10: no mapping turns a pending or unknown outcome into success or failure.
- Risks, and how they are contained: the existing runner already behaves this way, so the test is a guard, not a fix. A second NOT_ACHIEVED ends in EVALUATING with the recovery budget spent (a stop, not a collapse), so the property draws at most `PERDMaxDispatchesPerKey - 1` retries.
- Tests added (REQ-STM-002, BD-10): the two above. Three mutants each fail them: PENDING_EXTERNAL finishing in COMPLETED, UNKNOWN taking the ACHIEVED path, and a BLOCKED_UNKNOWN to COMPLETED arrow added to the table.
- Regression cases added: the same tests. Debt entries: none. Conflicts raised: none.
- Not covered (the REQ's other half): "the UI shows distinct copy for each" and the B2 metric belong to the UI task and the benchmark; this test does not claim them.

## Pre-work for the next agent
- Exact next step: none for STM-02's done-when; the Steward marks it on merge.
- Files to read first: `orchestrator/runwrite.go` (verify), `orchestrator/transitions.yaml`.
- Do not touch: `transitions.yaml` without the PERD text.
