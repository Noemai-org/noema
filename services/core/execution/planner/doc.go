// Package planner: Deterministic plans from registered capability nodes only, with a canonical plan hash. A
// compound request is planned, never executed beyond the grant.
//
// Implements: REQ-PLN-001, REQ-PLN-002, REQ-PLN-003, REQ-AG-001.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, internal/predicate,
// execution/registry.
//
// Must never: plan an unregistered or PROHIBITED capability; choose beyond the grant; call a model; import
// trust or outcome.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package planner
