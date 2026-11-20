// Package intakesources holds the production readers behind three consumer-side ports of the Trust intake's adapters (trust/intakeadapters, noema#747): the FactSource the policy
// conditions read, the Admissions port that gives the admission time of an intent version, and the at-risk fraction the Clocks adapter takes, which is the pack's clock_at_risk_remaining_bp read through platform/trustlimits (its one source: rulings A447 and A416). The Anchors port
// has no reader here: its data source does not exist yet (status/TRU-02-factsource.md).
//
// Implements: REQ-TRU-004, REQ-TRU-005, REQ-TRU-006, REQ-TRU-007 (production sources of T11 and T12), REQ-SEC-004 (each read is the tenant's own Trust login); A306, A345 (interim, relayed; S4 corrected by A447); INV-09, INV-11, INV-12, INV-14, INV-16, INV-19; AGENT-13, AGENT-16.
//
// May import: the standard library, pgx, internal/contracts, internal/predicate, platform/trustlimits, record/chain, trust/clocks, trust/decide, trust/intake, trust/intakeadapters.
//
// Must never: write, read with a login that is not the tenant's own Trust login (there is no fallback to another tenant's), give a fact it did not read (an absence is never a
// negative fact: INV-19), read a setting for a limit or default one, hold an industry term (INV-14), print a row, an ID, a tenant or a driver's error text in an error (AGENT-13), or read a clock.
package intakesources
