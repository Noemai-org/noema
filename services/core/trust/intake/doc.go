// Package intake is the Trust intake: it assembles the fifteen evaluation steps T1 to T15 of PERD 23.3 into one service that decides whether a request may go on
// (REQ-TRU-002; the G1 card of noema#436, ruling A205). The order, the stop rules, the trace and the fail-closed behaviour belong to trust/decide; the step logic that
// already exists (trust/clocks, trust/confirm, trust/policy, trust/reconcile) is called from here; and everything that lives outside Trust (the intent store, the
// capability registry, the pack, the evidence layer, the planner) is reached through the small interfaces of ports.go, so Trust imports none of those packages (INV-01,
// SoT 51). The composition root supplies the adapters.
//
// Implements: REQ-TRU-001, REQ-TRU-002, REQ-TRU-003 (T1 to T6 here), REQ-TRU-004 (T11), REQ-TRU-006 (T12), REQ-RES-001 and REQ-RES-002 (T10), REQ-NFR-002. Steps that are not
// built yet (T7 to T9, T13 to T15) fail closed: UNKNOWN INTERNAL_ERROR, never ALLOW.
//
// May import: trust/decide, trust/clocks, trust/policy, trust/reconcile, internal/contracts, platform/clock, the standard library.
//
// Must never: import execution, outcome, evidence, understanding, orchestrator or any provider or model package; read the wall clock (platform/clock only); read anything
// about a target before T7 (REQ-TRU-002: refuse before read); decide on a float; choose a threshold, which is pack data (REQ-OPS-003).
package intake
