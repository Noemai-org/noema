# Session 2026-11-23 07:30 WAT · TRU-10 (C1, ruling A534: an ESCALATE is recorded through the Recorder; the continuation names its disposition)

## Session summary
- What changed (PR links): tests first so far (the PR is Draft): (1) an ESCALATE outcome is recorded through the Recorder port with no step and no grant, and its decision ID comes back in `Result.DecisionID`; (2) `ContinuationRequest.DispositionDecisionID` (optional, field only so far) passed into the Decision Record's request; (4) a replayed ESCALATE returns the stored disposition by the same decision key (an adapter test on the real Writer and database; it passes on main already, the idempotency of A186, and is pinned here).
- Why: A534 (interim, relayed by the Founder, not yet committed to `docs`): the escalation path needs a disposition decision ID to hand to the action decisions that follow it. (3), the rescope of noema#733 (the disposition rule applies only to a decision whose steps include a capability of the injected escalation set; a `log_complaint_reference` ALLOW with a grant and no disposition is accepted), is NOT in this PR: the Founder's order is (1) and (2), then EL's path, then #733.
- Tests added (REQ-TRU-010, REQ-RCV-003; INV-25, INV-21, EC-E16): `trust/intake/escalate_recorded_test.go`, `orchestrator/portadapters/trust/recorder_disposition_test.go` (`TestAReplayedEscalateReturnsTheStoredDispositionByTheSameDecisionKey`).

## Pre-work for the next agent
- Exact next step: GREEN in `trust/intake` (wrap the steps so that an ESCALATE is recorded before it is returned; `decide.StepResult.Decision` accessor; `scopeOf` and T15 pass the disposition); then EL's escalation path; then the #733 rescope.
- Files to read first: `trust/intake/intake.go` (`run`), `trust/intake/gates_t13_t15.go` (`t15`), `trust/intake/ports_continuation.go`.
- Do not touch: `services/core/migrations/`.
