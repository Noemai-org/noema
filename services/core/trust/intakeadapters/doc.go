// Package intakeadapters holds the production adapters of two ports of the Trust intake: Policies (T11) and Clocks (T12). The ports are defined by their consumer, trust/intake
// (ports_t10_t12.go); this package implements them over the packages Trust already has (trust/policy, trust/policystore, trust/clocks) and the pack's data, and decides nothing the
// engines do not decide.
//
// Implements: REQ-TRU-004, REQ-TRU-005, REQ-TRU-006, REQ-TRU-007 (T11 and T12 production adapters); A306 (one policy evaluation for each capability of the authorized set, answered
// for that capability alone); INV-03, INV-09, INV-14, INV-16, INV-19, INV-21.
//
// May import: trust/intake, trust/policy, trust/policystore, trust/clocks, trust/decide, internal/contracts, internal/predicate, the standard library.
//
// Must never: hold a threshold, a TTL, an at-risk fraction or a routing rule (they come from the pack or the composition root as data: INV-14, REQ-OPS-003); read a clock (the query
// carries the reading); decide a policy version's order (the database does); read the intent store, the evidence layer or any enterprise source itself (the sources are the ports
// FactSource, Admissions and Anchors, which the composition root supplies); import execution, outcome, evidence, understanding or orchestrator.
package intakeadapters
