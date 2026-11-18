// Package intake is the Trust intake: it assembles the fifteen evaluation steps T1 to T15 of PERD 23.3 into one service that decides whether a request may go on
// (REQ-TRU-002; the G1 card of noema#436, ruling A205). The order, the stop rules, the trace and the fail-closed behaviour belong to trust/decide; the step logic that
// already exists (trust/clocks, trust/confirm, trust/policy, trust/reconcile) is called from here; and everything that lives outside Trust (the intent store, the
// capability registry, the pack, the evidence layer, the planner) is reached through the small interfaces of ports.go, so Trust imports none of those packages (INV-01,
// SoT 51). The composition root supplies the adapters.
//
// Implements: REQ-TRU-001, REQ-TRU-002, REQ-TRU-003 (T1 to T6 and T8 here), REQ-AMB-001 to REQ-AMB-004 (T7), REQ-CTX-003 (T9), REQ-RES-001 and REQ-RES-002 (T10), REQ-TRU-004 (T11),
// REQ-TRU-006 (T12), REQ-TRU-008 and REQ-TRU-009 (T13 to T15: the confirmation, the plan, the Decision Record and then the grants, through the Recorder port), REQ-NFR-002.
// Every step T1 to T15 is built; T15 is the only step that can end in ALLOW.
//
// May import: trust/decide, trust/clocks, trust/policy, trust/reconcile, internal/contracts, platform/clock, the standard library.
//
// Must never: import execution, outcome, evidence, understanding, orchestrator or any provider or model package; read the wall clock (platform/clock only); read anything
// about a target before T7 (REQ-TRU-002: refuse before read), and then only through the three ports of ports_t7_t9.go; decide on a float; choose a threshold, which is pack data (REQ-OPS-003).
package intake
