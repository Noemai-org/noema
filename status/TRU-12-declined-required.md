# Session 2026-11-23 12:00 WAT · TRU-12 (A542, a decline must name its request)

State: red pushed with the PR, green held locally (EL pushes it after reading the red job).

## Session summary
- What changed (PR links): not yet opened. Two commit pairs in one PR. (1) `trust/intake` `t13Declined` ends in UNKNOWN (the Given path's `errAnswer`) when a Declined answer's `RequestID` is empty or not a `cfr_` id. (2) The Recorder adapter's `declinedShape` refuses a REJECT with reason DECLINED_BY_APPROVER whose `DeclinedRequestID` is empty (ErrRecordRequest, before any transaction); every other shape still refuses a non-empty id.
- Why: A542 (interim, relayed, not yet committed; docs#231 records it). noema#892 left `declinedShape` returning nil on an empty id, and the intake let an empty id through (it checked the id only when non-empty), so a decline that names no request could be recorded.
- Reading taken (A332, safer): the intake refusal outcome is the Given path's (UNKNOWN, INTERNAL_ERROR at T13, StepFailed). The production Confirmations adapter always sets the id on Declined, so no production path was reaching the empty case; the intake and the adapter now agree anyway.
- Risks, and how they are contained: the change only narrows what is recorded. Edits are limited to `t13Declined` and its tests (noema#903 also edits trust/intake).
- Tests added (REQ-TRU-009, REQ-STM-001; INV-03): `TestADeclinedAnswerWithoutAWellFormedRequestIDIsUnknownAndNotRecorded`; `TestADeclinedRejectWithoutADeclinedRequestIDIsRefusedBeforeAnyTransaction`; `TestARejectForAnotherReasonMapsAnEmptyDeclinedRequest` replaces `TestARejectWithoutADeclinedRequestMapsEmpty`. The old intake assertion that a decline with no id is recorded with an empty id is removed.
- Test setup change: `rnaRequest` (recorder_nonallow_test.go) builds its REJECT with NOT_AUTHORIZED, no longer DECLINED_BY_APPROVER, because the real-Writer tests that use it (`TestRecordOfANonAllowWritesTheDecisionAndItsEventOnly`, `escalateDisposition`) have no stored request to name. A declined reject through the real Writer is covered in `trust/recorder`.
- Regression cases added: the two new tests above.
- Debt entries: none.
- Conflicts raised: none. Stale text, not edited (outside the one function): the comment on `DeclinedRequestID` in `trust/intake/ports_t13_t15.go` still says the field is optional for a decline.

## Pre-work for the next agent
- Exact next step: EL pushes the held green after reading the red job; DB tests (pgsodium) run in CI only.
- Open questions (owner): the stale comment above (C1/EL).
- Files to read first: `trust/intake/gates_t13_t15.go` (t13Declined), `orchestrator/portadapters/trust/recorder.go` (declinedShape).
- Do not touch: `trust/recorder`.
