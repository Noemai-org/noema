// Package understanding is the core's side of the Understanding boundary: the second schema gate on an Intent Object (REQ-INT-001), the overwrite of the
// principal and tenant from the session (REQ-INP-002), the intent hash, and the client of the model service's compile endpoint (PERD 39.3).
//
// Implements: UND-04 (core half), INV-02 (model output is untrusted data and crosses only the schema gate), INV-12 (identity comes only from the session),
// AGENT-09, AGENT-10.
//
// May import: internal/contracts, platform/crypto. It reads no pack file (the caller passes a PackView), holds no key and no credential, and writes nothing.
//
// Must never: repair, complete, re-type or infer a field of a model's output (REQ-INT-002); admit an object whose status is not VALIDATED; take an identity from
// the object; be reachable from trust, execution or outcome (they receive an AdmittedIntent, never raw output).
package understanding
