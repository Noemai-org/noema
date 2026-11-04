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
// Evaluate (TRU-06) is EVALUATE_SLA_STATUS of PERD 23.5: from a clock's data, its anchor time (nil when the anchor event has not
// happened: NOT_STARTED, never assumed), now and the at-risk fraction (an input from configuration, not held here), it returns
// started_at, deadline_at, elapsed, remaining and the phase. Integers only; it reads no clock. Not built here yet: the routing
// priority derived from the phase (pack routing rules; priority may be raised, never lowered) and the SLA_EVALUATED Record event,
// which arrive with the decision path (TRU-05).
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2); Evaluate by TRU-06.
package clocks
