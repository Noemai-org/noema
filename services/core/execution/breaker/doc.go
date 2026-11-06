// Package breaker: the per-capability limits of REQ-CAP-003, read from the capability descriptor: the call timeout, the circuit breaker
// (error rate over a window) and the per-principal hourly rate.
//
// Implements: REQ-CAP-003 (CAP-01 slice 1). SoT IDs: ASI08, SoT §43.8.
//
// May import: platform/clock, internal/contracts.
//
// Must never: read the time other than through the clock; invent a threshold (the descriptor gives the error rate, the window, the
// timeout and the rate; the minimum samples and the cooldown are required inputs with no default, AGENT-16); dispatch anything itself.
//
// An open circuit allows no call: the caller returns UNKNOWN or ESCALATE, never a guess. Recording CIRCUIT_STATE_CHANGED and wiring the
// breaker into the Execution Controller are later slices.
package breaker
