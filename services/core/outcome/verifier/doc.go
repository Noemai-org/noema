// Package verifier: Independent verification after the settle window, from the verifier's own reads,
// against the outcome definition.
//
// Implements: REQ-VER-001, REQ-VER-003.
//
// May import: platform/{clock,config,telemetry,redaction}, internal/contracts, internal/predicate,
// outcome/predicate, adapters (read-only identity).
//
// Must never: import anything under trust/ or evidence/ (the verifier-independence lint, INV-04); use a
// receipt, an HTTP status, a workflow completion or Trust's snapshot as proof; execute.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package verifier
