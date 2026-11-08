// Package export turns one correlation's Record into the neutral audit_record of benchmark-arm-result.v1 (task REC-09).
//
// Implements: REQ-AUD-003, REQ-BEN-001, REQ-BEN-009. SoT: INV-12 (the tenant comes from the session), INV-22 (nothing from the
// oracle or the held-out set), INV-11 (a chain that does not verify is refused).
//
// May import: platform/{crypto,clock}, record/chain.
//
// Must never: be an HTTP route (ruling A109: a library function the Noema arm runner calls); read the Record except through the
// tenant-bound record_read path; take the tenant from an argument other than the session; import the benchmark, the oracle or the
// held-out set; export payload text, or anything but the neutral fields.
//
// This file is a stub: the failing tests come first (AGENT-11) and the implementation follows.
package export
