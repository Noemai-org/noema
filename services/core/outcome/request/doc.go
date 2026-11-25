// Package request builds the Outcome verification request (LANE-5 slice 3; A547, A620, A635).
//
// Implements: REQ-VER-001, REQ-VER-003, REQ-OUT-001. SoT: INV-03, INV-04, INV-07, INV-12, AGENT-13, AGENT-21.
//
// Build is a pure function. From the facts of the Execution receipt row, the stored plan, the pack's outcome definition and the
// capability's retry safety, it returns the verifier.Request or a typed refusal. It reads nothing, writes nothing, owns no clock
// and decides no verdict: Outcome decides from its own reads (INV-04). A receipt is a pointer, so the builder refuses an unsigned
// receipt, one whose signature the caller did not find VALID, an ACKNOWLEDGED claim with no external acknowledgement, and any
// mismatch of tenant, intent or plan hash. A receipt that is not ACKNOWLEDGED binds nothing from the acknowledgement, so the
// verifier cannot reach ACHIEVED on it (INV-07).
//
// May import: the standard library's pure parts and outcome/verifier (its own types). Must never: import trust/, execution/ or
// evidence/ (the verifier-independence lint, SoT s51), open a file, a socket or a database, or read a clock. The composition (EL)
// maps the receipt row, the plan row and the pack into Inputs; the Record reader that supplies them is slice 4.
package request
