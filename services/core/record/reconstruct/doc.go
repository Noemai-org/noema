// Package reconstruct: The reconstruction endpoint: the causal narrative from the Record, and bit-for-bit
// decision replay (PERD v1.11).
//
// Implements: REQ-AUD-003, REQ-AUD-005, REQ-EXP-004.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, record/chain.
//
// Must never: serve data from a span whose chain verification fails (409 RECORD_INTEGRITY_FAILURE); call a
// model.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package reconstruct
