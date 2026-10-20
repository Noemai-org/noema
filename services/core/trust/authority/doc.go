// Package authority: Target-bound, three-layer authority (technical, policy, business) evaluated against
// the target resource, never against the request alone.
//
// Implements: REQ-TRU-003, REQ-TRU-002, REQ-SEC-001.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, evidence (read-only).
//
// Must never: take identity from anything but the authenticated session (INV-12); report PASS for a layer
// it did not evaluate; allow when any layer is not PASS (fail closed); call a model.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package authority
