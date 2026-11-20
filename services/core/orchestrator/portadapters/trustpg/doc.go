// Package trustpg is the Postgres side of the Trust adapter of noema#710 (STM-01, REQ-TRU-009, REQ-RCV-003; INV-03, INV-12): the three readers that adapter consumes, each as the
// tenant's own login, so the database and not this code decides what a tenant can see (REQ-SEC-004, DEBT-04).
//
//   - Intents implements trustport.Intents over the admitted-intent store (understanding/intentstore), as the tenant's trust_rw login.
//   - Records implements trustport.Records over decision_record, decision_grant and admitted_intent, as the same login, under forced row-level security.
//   - KeyDispatches implements trustport.KeyDispatches over the Record, as the tenant's record_read login.
//
// It reads and writes nothing else: no INSERT, UPDATE or DELETE anywhere, no key, no secret. A tenant with no login is ErrNoPool and never another tenant's pool.
package trustpg
