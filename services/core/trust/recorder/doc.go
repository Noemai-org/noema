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
// Not built here: CLARIFY (its clarification is not mapped to the contract), a refusal before any evidence is read (decision-record.v2 requires a snapshot
// and a policy, which T1 to T6 do not have: a gap in the contract, named in status/TRU-05.md), KEY_ROTATED and key loading.
package recorder
