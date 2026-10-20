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
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package recovery
