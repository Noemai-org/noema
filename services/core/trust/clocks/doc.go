// Package clocks: Clock evaluation for decisions: SLA and deadline clocks read from the synthetic clock
// source.
//
// Implements: REQ-TRU-006, REQ-ENV-003.
//
// May import: platform/{clock,config,telemetry}, internal/contracts.
//
// Must never: read the wall clock except through platform/clock (INV-21); decide anything; change a clock
// or an SLA in the enterprise.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package clocks
