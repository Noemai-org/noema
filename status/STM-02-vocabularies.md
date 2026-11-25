# Session 2026-11-25 · STM-02 (C1: the three tests that #941 does not carry)

## Session summary
- What changed (PR links): one test file, `services/core/orchestrator/stm02_test.go`, and this note. No production code changed. #941 (IL) merged the verdict-order property and the table-arrow test. These three are the rest of what A633 asked of C1.
- Why: REQ-STM-002 and BD-10: no mapping turns a pending or unknown thing into a success or a failure.
  1. `TestInvestigationStatesAndActionStatesAreSeparateVocabularies`: no investigation state is also a verdict, a receipt class or a decision outcome.
  2. `TestNoPendingStateIsShownAsAnswered`: the public stage of each pending state is never `answered`, and a watched outcome, an unknown decision and a completed investigation show three different stages.
  3. `TestNoRandomOrderOfReceiptsAndVerdictsCollapsesAPendingOrUnknownStateIntoAClosedOne`: random orders of decisions, receipts and verdicts through the real runner and table; COMPLETED only after EXPLAINING; an unknown receipt always goes to verification; PENDING_EXTERNAL ends in AWAITING_EXTERNAL_OUTCOME; UNKNOWN and COMPENSATED end in RECOVERING with nothing dispatched after.
- Risks, and how they are contained: guards over existing behaviour; they pass on `main` and cannot be seen red first. Two hand mutants were run and each failed its test: BLOCKED_UNKNOWN mapped to the `answered` stage (test 2), and a PENDING_EXTERNAL verdict finishing in COMPLETED (test 3). The rapid counterexample the mutant wrote was deleted, not committed.
- Tests added (REQ-STM-002, BD-10; INV-07, AGENT-21): the three above. Regression cases added: the same tests. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none for STM-02's done-when; the Steward marks it on merge.
- Files to read first: `orchestrator/runwrite.go` (`verify`), `orchestrator/transitions.yaml`, `orchestrator/publicstage`.
- Do not touch: `transitions.yaml` without the PERD text.
