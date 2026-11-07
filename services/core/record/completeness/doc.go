// Package completeness: the completeness check per consequential decision.
//
// Implements: REQ-AUD-002 (every governed write has the full event set), SoT §38.2 (consequential decisions with a complete causal
// record: 100%). SoT IDs: INV-10.
//
// May import: record/chain (the Row type only).
//
// Must never: read the database itself, repair or add an event, or treat a correlation it cannot judge as complete.
//
// Built by REC-04 (slice 1): the pure check over rows already read and verified (chain.ReadChain, chain.VerifyRows) and the
// metric hook for noema_audit_incomplete_total. Wiring it into the verification run, the benchmark audit and the reconstruction
// endpoint is a later slice.
package completeness
