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
// started_at, deadline_at, elapsed, remaining and the phase. Integers only; it reads no clock. Prioritise (routing.go) derives the
// routing priority and owner queue of a status through the pack's priority routing member (the consumer-side PriorityLookup): priority is raised and never lowered, a
// phase with no entry is NORMAL with no queue, and an empty queue is no re-route. Evaluation.EventPayload is the payload of the SLA_EVALUATED Record event, which
// trust/recorder appends (A632, A640, interim, relayed, not yet committed). Not built here: the call site in the decision path (T12).
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2); Evaluate by TRU-06.
package clocks
