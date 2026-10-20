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
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package controller
