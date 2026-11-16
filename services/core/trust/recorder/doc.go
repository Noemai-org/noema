// Package recorder writes a Decision Record and the grants of an ALLOW into the Record, in one transaction the caller supplies (TRU-05).
//
// Implements: REQ-TRU-008, REQ-TRU-001, REQ-AUD-001, REQ-SEC-005.
//
// May import: platform/{clock,crypto,ids}, internal/contracts, record/chain, trust/decide, trust/grants.
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
//
// Not built here: CLARIFY (its clarification is not mapped to the contract), a refusal before any evidence is read (decision-record.v2 requires a snapshot
// and a policy, which T1 to T6 do not have: a gap in the contract, named in status/TRU-05.md), KEY_ROTATED and key loading.
package recorder
