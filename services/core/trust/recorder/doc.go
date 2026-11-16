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
// A given confirmation is spent once (TRU-07 follow-up, migration 0028; REQ-TRU-008, REQ-TRU-009; INV-17): when the request names a confirmation event
// (Request.ConfirmationEventID) and the decision is an ALLOW whose row is newly written, one confirmation_spend row is inserted right after the Decision Record,
// in the same transaction. Its unique key (tenant, confirmation event) makes a second decision that names the same confirmation fail with ErrConfirmationSpent,
// however two evaluations interleave. A retry of the same input returns the stored decision before this insert (A186), so it spends nothing. Every grant
// follows the confirmation the decision spent: the writer sets it, a step's own claim is not used. A stored ALLOW is returned for the same input only when the retry names the
// confirmation it spent (or none, as it did): another is ErrDecisionConflict (A233, interim), with nothing spent or written. The writer also refuses an ALLOW whose confirmation
// was given for another plan than the one its steps carry (A230, interim): Request.ConfirmationPlanHash must equal every step's plan hash, else ErrConfirmationPlanMismatch before
// anything is written; the spend records that plan hash.
//
// Not built here: CLARIFY (its clarification is not mapped to the contract), a refusal before any evidence is read (decision-record.v2 requires a snapshot
// and a policy, which T1 to T6 do not have: a gap in the contract, named in status/TRU-05.md), KEY_ROTATED and key loading.
package recorder
