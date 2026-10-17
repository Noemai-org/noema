// Package trust decides whether a specific action on a specific target should become real now: reconciliation at decision time, authority, policy, clocks, the decision itself, grants and confirmations (SoT §13; PERD §23). Deterministic, with no model.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, and evidence (read-only).
//
// Must never: import execution, outcome or any model, explanation or provider client (INV-01); execute, change an intent, mark an outcome achieved, use a float type for anything it decides on, or read the wall clock except through platform/clock.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package trust
