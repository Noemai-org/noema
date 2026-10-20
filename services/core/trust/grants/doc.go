// Package grants: The one place a Decision Grant is built, signed and bound to its decision, plan and
// parameters; single use is enforced in the database, not here.
//
// Implements: REQ-TRU-008, REQ-TRU-005, REQ-SEC-005, REQ-RES-002.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, trust/decide.
//
// Must never: let any other package construct a DecisionGrant (the construct-decision-grant lint); issue a
// grant for a decision that is not ALLOW, or for a non-allowed capability on a frozen target (REQ-RES-002);
// hold a key outside platform/crypto.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package grants
