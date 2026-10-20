// Package registry: The Capability Registry: complete descriptors only; prohibited capabilities are
// registered, reject, and have no callable path; limits, timeouts and circuit breakers per capability.
//
// Implements: REQ-CAP-001, REQ-CAP-002, REQ-CAP-003.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts.
//
// Must never: load an incomplete descriptor; expose any route or adapter for a PROHIBITED capability
// (BD-08, INV-05); move money.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package registry
