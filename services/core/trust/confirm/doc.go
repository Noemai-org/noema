// Package confirm: Confirmation requests and events: binding hash, approver eligibility and expiry.
//
// Implements: REQ-TRU-009, REQ-SEC-005.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, trust/grants.
//
// Must never: accept an approval that does not match the binding hash, comes from an ineligible approver,
// or arrives after expiry; treat silence as a yes.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package confirm
