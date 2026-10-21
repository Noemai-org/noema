// Package controller: Spend-and-dispatch (PERD §27.1): verify the grant, persist the idempotency key and
// spend the grant in one transaction, revalidate, make a conditional write, write the receipt.
//
// Implements: REQ-EXE-001, REQ-EXE-002, REQ-EXE-003, REQ-EXE-004, REQ-EXE-005, REQ-EXE-006, REQ-EXE-007,
// REQ-EXE-008.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts,
// execution/{planner,registry}, adapters, evidence (revalidation reads).
//
// Must never: dispatch before the key and the spend are committed; retry without a new grant (AGENT-08);
// report anything but OUTCOME_UNKNOWN on silence; declare success (only Outcome does); import trust or
// outcome.
//
// Built by T6.5 of DEC-2026-10-20. Everything outside the process (the store, the grants, the target) is a
// port; the Postgres and Temporal implementations wait for the dependency decision of AGENT-14.
package controller
