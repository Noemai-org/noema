// Package readseal is the Evidence Layer's adapter for the orchestrator's Evidence port: it reads one admitted intent's transaction through the read ports, has the
// evidence classified, seals the snapshot and returns its ID (ENV-04 wiring slice W2; REQ-CTX-001, REQ-CTX-004, REQ-AUD-001; INV-11, INV-12, INV-17, INV-19).
//
// Implements: INV-12 (the Scope is built from the authenticated session, never from the intent), INV-11 (an intent or a session of another tenant is refused before any
// read), INV-17 (an empty set is never sealed; the snapshot and its Record event stand or fall together), INV-19 (a source that cannot answer is reported as
// Unavailable and never as a fact; what that means is Trust's decision, not this package's).
//
// May import: orchestrator (its Session and EvidenceRef only), adapters (Scope), evidence/layer, evidence/portread, evidence/snapshot, understanding/intentstore.
//
// Must never: decide, derive a status (layer does), classify (the Reconciler does, and it has no default here), read a pack file, write to an enterprise system,
// or name an industry in a literal. cmd/core plugs this in; the orchestrator never imports it.
package readseal
