// Package memory: Operational memory: append-only structured observations and regression knowledge, as
// data.
//
// Implements: REQ-MEM-001, REQ-MEM-002.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, record/chain.
//
// Must never: change policy or behaviour (memory is data, never authority); learn or adapt automatically.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package memory
