// Package recorder writes a Decision Record and the grants of an ALLOW into the Record, in one transaction the caller supplies (TRU-05).
//
// Implements: REQ-TRU-008, REQ-TRU-001, REQ-AUD-001, REQ-SEC-005, REQ-TRU-010, REQ-EXE-008 (Trust side only), REQ-OPS-002 (Trust side only), REQ-TRU-009 (the confirmation request of a CONFIRM).
//
// May import: platform/{clock,crypto,ids}, internal/contracts, record/chain, trust/confirm, trust/decide, trust/grants.
//
// Must never: commit or roll back the caller's transaction; issue a grant for a decision that is not ALLOW; write a decision whose basis is not the set the
// evaluation recorded (TRU-13); use a tenant, an identity or a key that the request names for another purpose than the one it is checked for.
//
// Order inside Write, which is the order of the Record-first rule (PERD §32.4, EC-E16): the request and the basis are checked; DECISION_MADE is appended; the
// signed Decision Record (decision-record.v2) is inserted; then for each step the grant is issued, inserted, and its GRANT_ISSUED event appended. Any failure
// is returned and the caller rolls back, so the record, the grants and the events appear together or not at all. A failed append fails the write, so a
// grant never exists without its event.
//
// A given confirmation is spent once, and only if it is valid for the action (TRU-07 follow-up, migration 0028; REQ-TRU-008, REQ-TRU-009; INV-17, INV-25). The confirmation store records an
// accepted confirmation that resolved to a fresh evaluation with RecordConfirmation: its event ID, its request ID, the binding hash the request was given for (trust/confirm.BindingHash of the
// eight values of SoT 43.2a) and its expiry. When a request names one (Request.ConfirmationEventID) and the decision is an ALLOW whose row is newly written, the writer computes the binding from the
// ALLOW's own single step (a confirmation approves one action) and inserts one confirmation_spend row right after the Decision Record, in the same transaction. The database refuses the row unless
// the confirmation exists for the tenant, has not expired by the database's clock and has that binding (ErrConfirmationNotValid: the three are one answer), and its unique keys make a second spend
// of the event, or a second spend by the decision, ErrConfirmationSpent, however two evaluations interleave. Request.ConfirmationPlanHash must equal the step's plan hash, else
// ErrConfirmationPlanMismatch before anything is written (A230, interim). A retry of the same input returns the stored decision before this insert (A186) when it names the confirmation the
// stored ALLOW spent (or none, as it did): another is ErrDecisionConflict (A233, interim). PERD 32.2 has no spend event type, so the Record shows the spend on two events of the same
// transaction: DECISION_MADE names confirmation_event_id and confirmation_binding_hash, GRANT_ISSUED names confirmation_event_id; a decision with no confirmation carries neither.
// What the database does NOT enforce (Security Reviewer, noema#603 note 1): that an ALLOW for a profile that requires a confirmation carries one. An ALLOW with an empty ConfirmationEventID
// writes no spend and is not checked here; that rule is the intake's (T13, which stops with CONFIRM when the profile requires one) and the Recorder adapter's, and a composition test of the two
// owes the case "confirmation required, none named, ALLOW refused" (EL's adapter, with noema#572).
//
// The tenant's kill switch (EXE-06, PUS-06; REQ-EXE-008, REQ-OPS-002; ruling A297, interim): a request with steps is refused with ErrWritesDisabled, before anything is written, while
// tenant.kill_switch is on for the request's tenant. The writer reads the column on the caller's transaction (it has no other connection) after taking the input lock and before the
// stored decision is read or anything is appended, so a refusal leaves no decision row, no grant and no event, and a retry of a stored decision with steps is refused too. A request with no
// step is not refused: a read, an explanation or a refusal records as before. A switch that cannot be read is an error, never "off". The read is a plain SELECT at the transaction's own
// isolation level and holds no lock on the tenant row (the Trust login has SELECT only, and FOR SHARE needs UPDATE), so a switch turned on after the read and before the commit is not seen
// here; Execution reads it again inside the spend (E6, E8) and voids the grant, so the window never reaches a write. Not built here: the control plane that sets and clears the switch, and
// any KILL_SWITCH_* event (KILL_SWITCH_CHANGED, KILL_SWITCH_CLEARED, KILL_SWITCH_CLEAR_REFUSED are in contracts/perd-32-2-event-types.md; this writer emits none of them).
//
// The confirmation request of a CONFIRM (TRU-07 PR 2; REQ-TRU-009, INV-25, INV-12, EC-E16; SoT 43.2a; interim defaults of the founder's order, not yet committed). A CONFIRM decision may carry
// Request.ConfirmationRequest: the eight bound values (principal, intent hash, capability, target, parameters hash, evidence set hash, plan hash, policy version), the approver requirement
// (SELF, MAKER_CHECKER or ROLE:<role>), the requester and the expiry in Unix seconds. Any other outcome that carries one is ErrRequest. Before anything is written the writer checks the
// hashes against the contract's form, that the intent hash is the decision's own and the policy version is the COMPOSITE of every policy version of the decision's basis (decide.CompositePolicyVersion, A341, A367: "sha256:" and the
// SHA-256 hex of the RFC 8785 form of the sorted, distinct "policy_id@version" list; the ALLOW that follows an ACCEPT recomputes the binding from the same function, so a request bound to
// others could never be accepted; the decision record and the grant still bind the one policy the request names), that the binding can be computed (trust/confirm.BindingHash refuses an empty value or one outside its character set), the requirement, a
// requester, and that the expiry is after the writer's own clock; any failure is ErrRequest. Then, on the new-decision path only, it mints the cfr_ ID with its own generator (the caller
// never supplies one) and computes the binding hash itself (never a caller's), names the request in the DECISION_MADE payload (confirmation_request_id) and appends CONFIRMATION_REQUESTED
// right after DECISION_MADE, by the Trust emitter, in the same transaction: the decision, its request and its events commit together or not at all. The payload is confirmation_request_id,
// decision_id, binding_hash, principal_id, intent_hash, capability_id, target_ref, params_hash, evidence_set_hash, plan_hash, policy_version, approver_requirement, requester_id and
// expires_at (an RFC 3339 timestamp, as in SoT 43.2a), all identifiers, hashes or the expiry (policy_version is the composite policy version in the content-hash form "sha256:<hex>", A367, a typed value the payload guard, A184, exempts), so the confirmation store can rebuild the request from the event; the object_ref is the
// request ID. Result.ConfirmationRequestID is the minted ID; it is empty when a stored decision of the same input was returned (A186): that path creates no event and no second request, and
// the first request is in the Record. A CONFIRM that names an already open request carries no ConfirmationRequest and writes no request event. The request is not a grant and no step
// accompanies it. The payload guard is not weakened: a target reference is accepted at /target_ref (ruling A193) and any other string with a run of more than four digits is refused (the
// append fails and so does the write). Not built: the display and the signature of confirmation-request.v1 (the approver's view is the confirmation store's), expiry (CONFIRMATION_EXPIRED)
// and the answer (CONFIRMATION_RECEIVED, RecordConfirmation).
//
// A186 and the second decision of a tuple (flagged to the Architect, status/TRU-07-confirm-t13.md): one decision exists for an input tuple, and migration 0027 enforces it, so after a
// stored CONFIRM an ALLOW or a REJECT for the SAME tuple is ErrDecisionConflict, not a new decision. A re-evaluation after an ACCEPT or a DECLINE must therefore read a new sealed snapshot.
//
// Disposition linkage (TRU-10; REQ-TRU-010, INV-25; PERD 23.1; ruling A297, interim). An action decision is the per-write-step decision of an escalation: the decision of one step of the
// escalation plan, which names its ESCALATE disposition in Request.DispositionDecisionID. Two rules, both refused before anything is written (no row, no Record event; the caller still
// rolls back). (1) A decision that names a disposition names an existing ESCALATE decision of its own tenant (read in the caller's transaction; a missing decision, another tenant's and one
// with another outcome are one answer, ErrDispositionNotValid). The database's foreign key (migration 0012) says the same behind it and its failure maps to the same error. (2) Every step of
// such a decision must carry a capability of the pack's escalation plan, which the composition root injects as Writer.Escalation (an EscalationSet built from pack data; this package holds no
// capability ID, INV-14). A step outside the set is ErrStepOutsideEscalation; the zero set allows no step. A decision that names no disposition is written as before, with or without steps: PERD
// 23.1 does not say whether such an ALLOW (the complaint write of another graph, for one) needs a disposition, so none is required (open reading, status/TRU-10.md). Building the set from the
// pack's escalation plan is the production adapter's job and is not built here. The disposition's intent is not compared with the action decision's: the PERD gives the disposition "for the
// investigation intent" and each step its own decision, so the two need not share an intent ID.
//
// Not built here: CLARIFY (its clarification is not mapped to the contract), a refusal before any evidence is read (decision-record.v2 requires a snapshot
// and a policy, which T1 to T6 do not have: a gap in the contract, named in status/TRU-05.md), KEY_ROTATED and key loading.
package recorder
