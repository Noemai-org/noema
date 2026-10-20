// Package callbacks: Signed outcome.updated callbacks with bounded retry to allow-listed targets.
//
// Implements: REQ-VER-005, REQ-API-006, REQ-SEC-009.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts.
//
// Must never: call a target that is not on the allow-list; send an unsigned event; retry beyond the budget;
// leak unmasked data.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package callbacks
