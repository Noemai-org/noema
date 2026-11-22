# STM-01 · the Recorder adapter records every outcome

State: draft PR noema#840; red pushed (f0774dfc), green committed locally, to be pushed by EL.

# Session 2026-11-20 · STM-01 · the Recorder adapter, non-ALLOW outcomes

## Session summary
- What changed (PR links): noema#840. `RecorderAdapter.request` no longer demands a plan hash of every outcome: an ALLOW needs it; a REJECT, CONFIRM or UNKNOWN has none, no step and so no grant (any of them, or a confirmation request on a non-CONFIRM, is refused before the transaction). The CONFIRM's `ConfirmationRequest` is now handed to the Writer, and the minted `ConfirmationRequestID` comes back.
- Why: noema#839 (the Decide lane): #759's CONFIRM and #755's REJECT would fail closed as UNKNOWN INTERNAL_ERROR in production. A456 (interim, relayed, not yet committed).
- Risks, and how they are contained: the adapter cannot tell a read ALLOW from a write ALLOW with its steps dropped, so the step count for a write stays the intake's check. RecordRequest has no DeclinedRequestID, so a REJECT is recorded without one (optional for the Writer).
- Tests added (REQ-TRU-002, REQ-NFR-002; INV-03, INV-17, EC-E16): `recorder_nonallow_test.go`. 8 hand mutants, all killed. The mapping of ConfirmationBindingEvidenceHash (A435) is owned by noema#833 (A459: from the sealed snapshot via RecordFacts), not this PR; the test that asserted it was removed from the green (it is in the pushed red).
- Regression cases added: the four outcomes with no steps and no plan hash.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: nothing in this lane; the testpg tests run in CI only (pgsodium).
- Open questions (owner): a DeclinedRequestID on the intake's RecordRequest (C1, trust/intake).
- Files to read first: `orchestrator/portadapters/trust/recorder.go`.
- Do not touch: `trust/**`.
