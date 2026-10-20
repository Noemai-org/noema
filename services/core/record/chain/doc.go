// Package chain: The append-only, hash-chained Record: per-tenant serialised appends in the transaction of
// the object write, completeness per consequential decision, the correlation chain.
//
// Implements: REQ-AUD-001, REQ-AUD-002, REQ-AUD-004.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts.
//
// Must never: update or delete a row; append outside the transaction of the object write; act on anything
// (the Record never acts).
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package chain
