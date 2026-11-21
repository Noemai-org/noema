// Package portread is the Evidence Layer's call site for the read ports of the adapters: it reads one transaction's ledger, switch and destination
// answers and records each as evidence through the Builder (ENV-04 wiring slice W1; REQ-CTX-001, REQ-CTX-002, REQ-ENV-001, REQ-AUD-001).
//
// Implements: INV-12 (the Scope is an argument the caller takes from the authenticated session; nothing here reads an identity from anywhere else),
// INV-19 (an absent, unavailable or unreadable answer is never a fact), INV-23 (the freshness limit is the pack's), AGENT-25 (the synthetic label
// travels from the adapter's provenance to the Record).
//
// May import: adapters (read ports), evidence/layer, platform/clock.
//
// Must never: write to an enterprise system, decide, derive a status (layer.AssignStatus does), read a pack file (the caller passes the descriptors it
// took from the pack), or name an industry in a literal (the resource kind comes from the pack descriptor).
package portread
