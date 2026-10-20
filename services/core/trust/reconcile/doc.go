// Package reconcile: Reconciliation at decision time: a pure, versioned function (evidence_set,
// ruleset_version, clock) giving a classification, the rule that fired, the contributing evidence, the
// contradictions and valid_until. Rule set v1 is pack data in the predicate language (PERD §22.3).
//
// Implements: REQ-RES-001, REQ-RES-002, REQ-RES-003, REQ-RES-004, REQ-STM-002, REQ-TRU-007.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, internal/predicate.
//
// Must never: average, order-pick or merge incompatible facts; do I/O; read the wall clock except through
// platform/clock; collapse a pending or unknown state into a terminal one; import execution, outcome or any
// model client.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package reconcile
