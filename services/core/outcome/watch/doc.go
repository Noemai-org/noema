// Package watch: Durable outcome watches with deadlines for pending outcomes.
//
// Implements: REQ-VER-004, REQ-STM-002.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, workflows (through
// its interface).
//
// Must never: hold authoritative state in Temporal (INV-30); collapse a pending state into a terminal one;
// execute.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package watch
