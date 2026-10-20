<!-- Vendored extract of Noemai-org/docs docs/poc/perd/NOEMA_POC_PERD_v1.10.md section 28.2 (Investigation state machine) at docs commit d9be3e4d321db76037a78576d021c2ac0e553e31. The PERD original is normative; this copy exists so a test can check transitions.yaml against it without cross-repository access. Re-point every copy with: python3 tools/vendor/repoint_docs.py --docs <docs checkout> -->

### 28.2 Investigation state machine [ADOPTED DEC-2026-10-17]

The investigation state is a **projection**. Every transition is caused by, and cites, a Record event and a power's signed object. No transition can be made by setting a field; it is made only by a named command that checks the guard. **Noema's investigation reaching COMPLETED never closes or resolves the enterprise case** (CLOSE_OR_RESOLVE_CASE is prohibited).

Each state below lists what it means, what moves the investigation into and out of it, what is forbidden, what the customer is told, and its audit trail.

**RECEIVED**

- Meaning: input accepted and deduplicated.
- Entry: `INPUT_RECEIVED`.
- Exit and allowed next states: → INTERPRETING.
- Prohibited: anything else.
- Customer is told: "Got it."
- Audit: INPUT_RECEIVED.

**INTERPRETING**

- Meaning: the model service is compiling.
- Entry: compile requested.
- Exit and allowed next states: → AWAITING_CLARIFICATION (an intent-level clarify via Trust), EVALUATING (admitted), REJECTED_AT_GATE.
- Prohibited: → PLANNED or EXECUTING.
- Customer is told: "Understanding your request."
- Audit: INTENT_PROPOSED / ADMITTED / REJECTED_AT_GATE.

**REJECTED_AT_GATE** (terminal for this turn)

- Meaning: the schema gate failed.
- Entry: gate failure.
- Exit and allowed next states: → INTERPRETING on the next turn (at most 2 times), then → ESCALATED (handoff).
- Prohibited: → EVALUATING without a new valid version.
- Customer is told: "I didn't catch that. Could you say it another way?"
- Audit: INTENT_REJECTED_AT_GATE.

**AWAITING_CLARIFICATION**

- Meaning: a CLARIFY decision is open.
- Entry: DECISION_MADE (CLARIFY).
- Exit and allowed next states: → INTERPRETING (answer), DORMANT (inactivity), ESCALATED (round limit).
- Prohibited: any write; → PLANNED.
- Customer is told: one question.
- Evidence required: candidate set version.
- Audit: CLARIFICATION_*.

**EVALUATING**

- Meaning: Trust is reading evidence, reconciling and deciding.
- Entry: intent admitted, a RecoveryRequest, a WAIT timer, or a confirmation ACCEPT.
- Exit and allowed next states: → AWAITING_CLARIFICATION, AWAITING_CONFIRMATION, WAITING, PLANNED, EXPLAINING, ESCALATING, REJECTED, BLOCKED_UNKNOWN.
- Prohibited: → EXECUTING directly.
- Customer is told: the honest progress states.
- Evidence required: a sealed snapshot.
- Audit: EVIDENCE_READ, STATE_RECONCILED, POLICY_EVALUATED, DECISION_MADE.

**AWAITING_CONFIRMATION**

- Meaning: a confirmation request is open.
- Entry: DECISION_MADE (CONFIRM).
- Exit and allowed next states: → EVALUATING (ACCEPT), REJECTED (DECLINE), EVALUATING (EXPIRED; a fresh decision follows) [ADOPTED DEC-2026-10-17].
- Prohibited: → PLANNED without re-evaluation (INV-25).
- Customer is told: the confirmation card.
- Evidence required: binding hash.
- Audit: CONFIRMATION_*.

**WAITING**

- Meaning: WAIT decided; a re-evaluation timer is set.
- Entry: DECISION_MADE (WAIT).
- Exit and allowed next states: → EVALUATING (timer or triggering event).
- Prohibited: any write.
- Customer is told: why we are waiting and until when.
- Evidence required: `reevaluate_at`.
- Audit: DECISION_MADE.

**PLANNED**

- Meaning: an ALLOW action decision exists and grants are issued.
- Entry: GRANT_ISSUED.
- Exit and allowed next states: → EXECUTING; → EVALUATING (grant voided).
- Prohibited: → COMPLETED.
- Customer is told: "Deciding the safest next step."
- Evidence required: grant.
- Audit: PLAN_CREATED, GRANT_ISSUED.

**EXECUTING**

- Meaning: a grant is spent and dispatched.
- Entry: GRANT_SPENT.
- Exit and allowed next states: → AWAITING_VERIFICATION (any receipt).
- Prohibited: → EXECUTING again for the same grant; → COMPLETED.
- Customer is told: "Opening a case."
- Evidence required: idempotency record.
- Audit: GRANT_SPENT, DISPATCH_ATTEMPTED.

**AWAITING_VERIFICATION**

- Meaning: a receipt exists (including OUTCOME_UNKNOWN); Outcome waits out the settle window, then reads.
- Entry: RECEIPT.
- Exit and allowed next states: → (next plan step) PLANNED; EXPLAINING (all ACHIEVED); AWAITING_EXTERNAL_OUTCOME (PENDING_EXTERNAL); RECOVERING (any other verdict).
- Prohibited: → COMPLETED without an ACHIEVED verdict; → EXECUTING.
- Customer is told: "Confirming the case was opened."
- Evidence required: receipt.
- Audit: VERIFICATION_READ, VERDICT.

**AWAITING_EXTERNAL_OUTCOME**

- Meaning: a PENDING_EXTERNAL verdict with a durable watch.
- Entry: WATCH_CREATED.
- Exit and allowed next states: → EXPLAINING / COMPLETED (ACHIEVED); RECOVERING (deadline → UNKNOWN).
- Prohibited: being dropped silently (INV-28).
- Customer is told: "Pending, and here is what we're watching" (the conversation may end).
- Evidence required: watch with a deadline.
- Audit: WATCH_*.

**RECOVERING**

- Meaning: a RecoveryRequest is with Trust.
- Entry: RECOVERY_REQUESTED.
- Exit and allowed next states: → EVALUATING.
- Prohibited: → EXECUTING directly (INV-15).
- Customer is told: "Checking again."
- Evidence required: verdict.
- Audit: RECOVERY_REQUESTED.

**BLOCKED_UNKNOWN**

- Meaning: Trust UNKNOWN; an ops alert is raised; a re-evaluation timer is set.
- Entry: DECISION_MADE (UNKNOWN).
- Exit and allowed next states: → EVALUATING (timer, at most 2 times [ADOPTED DEC-2026-10-17]); ESCALATED (handoff).
- Prohibited: any write.
- Customer is told: "I can't confirm this right now; here is what happens next."
- Audit: DECISION_MADE, OPS_ALERT_RAISED.

**ESCALATING**

- Meaning: an ESCALATE disposition with the escalation plan in progress.
- Entry: DECISION_MADE (ESCALATE).
- Exit and allowed next states: → PLANNED (action decisions); ESCALATED (case verified, or handoff).
- Prohibited: a non-escalation write while frozen.
- Customer is told: "Passing this to the team that can act."
- Audit: DECISION_MADE (both).

**EXPLAINING**

- Meaning: the explanation is being rendered and validated.
- Entry: verdict(s) ready, or ALLOW read.
- Exit and allowed next states: → COMPLETED, ESCALATED or REJECTED (whichever disposition applies); → EXPLAINING_BLOCKED → fallback.
- Prohibited: a claim not bound to a fact.
- Customer is told: the answer.
- Audit: EXPLANATION_RENDERED / BLOCKED.

**COMPLETED** (terminal)

- Meaning: every desired outcome is ACHIEVED and the explanation has been delivered.
- Entry: verdicts ACHIEVED.
- Exit and allowed next states: → (new turn) a new linked investigation.
- Prohibited: reopening the same epoch.
- Customer is told: the verified result.
- Audit: INVESTIGATION_STATE_CHANGED.

**ESCALATED** (terminal for Noema)

- Meaning: the case was verified opened, or a handoff was delivered.
- Entry: case verdict ACHIEVED or handoff.
- Exit and allowed next states: → AWAITING_EXTERNAL_OUTCOME (if a watch exists).
- Prohibited: closing the enterprise case.
- Customer is told: the case reference, owner and clock.
- Audit: —

**REJECTED** (terminal)

- Meaning: a REJECT decision.
- Entry: DECISION_MADE (REJECT).
- Exit and allowed next states: → (new turn) a new investigation.
- Prohibited: any write.
- Customer is told: what Noema cannot do and what it can do.
- Audit: —

**DORMANT**

- Meaning: no activity for 30 minutes.
- Entry: timer.
- Exit and allowed next states: → INTERPRETING (resume, with re-reads); EXPIRED.
- Prohibited: a decision on old evidence.
- Customer is told: —
- Audit: —

**EXPIRED** (terminal)

- Meaning: 72 hours passed without a resume.
- Entry: timer.
- Exit and allowed next states: none.
- Customer is told: —
- Audit: —

**FROZEN** is not a separate investigation state. It is a target-level flag (§22.3, REQ-RES-002) that Trust checks in every state.
