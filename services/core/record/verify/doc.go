// Package verify: Nightly and on-demand chain verification, and signed chain-head checkpoints exported
// outside Postgres (PERD v1.11).
//
// Implements: REQ-AUD-005, REQ-AUD-001.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, record/chain.
//
// Must never: repair or rewrite a broken chain; keep a checkpoint only inside Postgres; ignore a break (it
// raises an alert and the kill switch).
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package verify
