// Package intentstore is the admitted-intent store (ruling A151): it keeps every Intent Object the second schema gate admitted, one row per version, and the Record
// event INTENT_ADMITTED that says so. Trust reads an intent by reference and Execution reads the hash of the latest admitted intent of an investigation at E3.
//
// Implements: REQ-INT-001 (only an admitted intent is stored), REQ-SEC-004 and INV-12 (the tenant is the login's: the database binds it), REQ-AUD-001 and EC-E16 (the
// Record event comes first, in the same transaction as the row; if either fails neither stands), INV-17.
//
// May import: record/chain, platform/clock. It takes the fields of an admitted intent as plain values (Admitted), so it does not import the gate; the caller copies
// them from understanding.AdmittedIntent. It holds no credential beyond the pool it is given: the orchestrator's tenant login (Store) or the tenant login of the reader's
// own power, Trust's or Execution's (Reader).
//
// Must never: store a document the gate did not admit; read or write across tenants; update or delete a row (the table refuses it for every role); put the text of an
// input in the Record event.
package intentstore
