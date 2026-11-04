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
// Slice 1 (TRU-05): Issuer.Issue builds, signs and returns the grant for one write plan step of an ALLOW decision with a complete basis;
// Keys holds one or two active HMAC keys (the first signs, any verifies; a retired key is absent). The signature is HMAC-SHA-256 over the
// JCS form of the grant without its sig, with the key_id. Not built here yet: persisting the Decision Record before any grant, the
// grants in the same transaction as their GRANT_ISSUED events, the database trigger that forbids a grant on a non-ALLOW decision, and the
// KEY_ROTATED event (they need the decision_record table and the Record writer; see status/TRU-05.md).
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2); Issuer and Keys by TRU-05.
package grants
