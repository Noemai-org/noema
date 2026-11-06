// Package verifier: independent verification after the settle window, from the verifier's own reads, against the
// outcome definition (OUT-01).
//
// Implements: REQ-VER-001, REQ-VER-003, REQ-RCV-002.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, internal/predicate. It reads the
// target only through its own CaseReader port, which has no write method; an adapter implements it, so the verifier does
// not import adapters (the lint rule import-adapters allows only execution, evidence and adapters; the earlier text of this
// file listed adapters, which the lint contradicts).
//
// Must never: import anything under trust/ or evidence/ (the verifier-independence lint, INV-04); use a receipt, an HTTP
// status, a workflow completion or Trust's snapshot as proof; execute.
package verifier
