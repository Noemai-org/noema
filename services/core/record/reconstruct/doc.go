// Package reconstruct: the reconstruction endpoint, the causal narrative of one correlation built from the Record
// alone (REQ-AUD-003; task REC-03).
//
// Implements: REQ-AUD-003. Shares the chain verifier with REQ-AUD-005 (record/chain.Verify).
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, record/chain.
//
// Must never: serve data from a span whose chain verification fails (409 RECORD_INTEGRITY_FAILURE); call a
// model; take the tenant from the request instead of the authenticated session (INV-12); serve a fact that is not
// in a Record payload (the model said so is never provenance).
//
// Bit-for-bit decision replay (REQ-EXP-004) is not built here yet.
package reconstruct
