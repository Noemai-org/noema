# TRU-12 · intake: T13 Declined and the confirmation request (PR noema#755)

State: merged origin/main (noema#757) into the branch; red commit 5ab44918 pushed; green committed locally, pushed by EL. REQ-TRU-009; INV-12, INV-16, INV-25; SoT 43.2a.

# Session 2026-11-19 WAT · TRU-12

## Session summary
- What changed (PR links): noema#755. After the merge of noema#757 only what #757 did not do remains: `decide/evaluate.go` T13 stop set {Confirm, Reject}; T13 Declined is REJECT DECLINED_BY_APPROVER with the evaluation's basis (replacing #757's interim "Declined is UNKNOWN" test); `Result.ConfirmationRequestID`; the `Recorder.RequestConfirmation` method and `ConfirmationRequest` type, which T13 calls for a Pending answer with a NewRequest.
- Why: the Founder's order of 19 Nov 2026 (SoT 43.2a, CR docs#209), interim, relayed, not yet committed.
- Risks, and how they are contained: the Recorder port gains a method, so its implementer (EL's composition) must provide it; any error, empty id or non-`cfr_` id is UNKNOWN (no CONFIRM without a request).
- Tests added: REQ-TRU-009, INV-12, INV-16, INV-25 in `t13_t15_test.go`, `t13_binding_test.go`.
- Regression cases added: none. Debt entries: none. Conflicts raised: the CONFLICT block of #755 is resolved by the Founder's decision.

## Pre-work for the next agent
- Exact next step: the Recorder adapter implements `RequestConfirmation` (persist the request and CONFIRMATION_REQUESTED in one transaction, return the cfr_ id, with a context deadline).
- Open questions (owner): the plan hash in the binding is unknown at T13 (the plan is built at T14, A230); the adapter must decide how the stored request is bound (EL).
- Files to read first: `trust/intake/gates_t13_t15.go` (`t13`, `requestID`), `ports_t13_t15.go`.
- Do not touch: `trust/recorder`, `/contracts`.

# Session 2026-11-20 WAT · TRU-12 (intake declined, review fix A435)

## Session summary
- What changed (PR links): noema#755. A Declined answer ends T13 in REJECT DECLINED_BY_APPROVER and the REJECT is recorded once through Recorder.Record (no step, no plan hash, no confirmation event), Record first; a Recorder error, panic or malformed answer ends UNKNOWN INTERNAL_ERROR, never a REJECT. Result.DecisionID is set for that REJECT (A435, interim, relayed, not yet committed).
- Why: A435: #755 owns the mapping and every decision is the Record's unit; the old "nothing is recorded" test is replaced.
- Risks, and how they are contained: Recorder.Record is documented for the ALLOW candidate; the writer accepts any outcome with no steps, but the composition adapter must accept a REJECT with empty PlanHash (safer reading, listed in the PR body). Tests: trust/intake only; the DB tests run in CI.
- Tests added (REQ-TRU-009, INV-16): TestT13DeclinedIsRejectDeclinedByApproverAndIsRecordedOnce, TestT13DeclinedThatCannotBeRecordedIsUnknownNeverReject.
- Regression cases added: those two. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Recorder adapter in the composition must write a REJECT with no steps and an empty plan hash.
- Open questions (EL): should Decide in portadapters/trust expose the REJECT's decision ID (it returns an ID only for ALLOW today)?
- Files to read first: trust/intake/gates_t13_t15.go (recordDecline). Do not touch: trust/decide.
