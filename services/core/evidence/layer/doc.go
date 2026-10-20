// Package layer: The Evidence Layer: read every authoritative source through adapters, tag each fact with
// provenance, FactStatus and both timestamps, and seal the context snapshot with its evidence_set_hash.
//
// Implements: REQ-CTX-001, REQ-CTX-002, REQ-CTX-004, REQ-CTX-005, REQ-CTX-008.
//
// May import: platform/{clock,config,telemetry,redaction}, internal/contracts, adapters (read-only).
//
// Must never: write to an enterprise system; decide; let an enterprise string become an instruction (REQ-
// CTX-007); import trust, execution or outcome.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package layer
