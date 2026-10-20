// Package policy: Versioned, effective-dated policy loader and evaluator, the precedence ladder and default
// deny. Publishing a policy voids the open grants it governed.
//
// Implements: REQ-TRU-004, REQ-TRU-005, REQ-OPS-003.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, internal/predicate.
//
// Must never: hold a threshold, TTL or margin as a literal (it comes from platform/config); evaluate a
// prose rule; treat a retired policy version as active; call a model.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package policy
