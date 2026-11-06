// Package recovery: Recovery after an uncertain or failed write: verify by key before any retry, a recovery
// budget, escalation, and requests that go to Trust only.
//
// Implements: REQ-RCV-001, REQ-RCV-002, REQ-RCV-003, REQ-RCV-004.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, adapters (read by
// key).
//
// Must never: start a write itself; retry without a new grant; skip the verify-by-key read; import trust or
// outcome.
//
// Built by RCV-01 (slice 1): Handler.Handle asks Trust, through a port, to evaluate a RecoveryRequest afresh and passes on only an ALLOW;
// a Trust failure, panic or UNKNOWN is ESCALATE with an ops alert. CheckRetryOrder checks over Record rows that every dispatch with
// attempt > 1 follows a non-ACHIEVED verdict and a new ALLOW. The Temporal recovery workflow that calls Handle (OUT-03), the recovery
// budget (RCV-02) and compensation (RCV-04) are later tasks.
package recovery
