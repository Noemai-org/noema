// Package chain: The append-only, hash-chained Record: per-tenant serialised appends in the transaction of
// the object write, completeness per consequential decision, the correlation chain.
//
// Implements: REQ-AUD-001, REQ-AUD-002, REQ-AUD-004. Task REC-01 (append, chain-head lock, genesis hash) and the
// Verify function the chain verifier (REC-05, REC-06) calls.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts.
//
// Must never: update or delete a row; append outside the transaction of the object write; act on anything
// (the Record never acts).
package chain
