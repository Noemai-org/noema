// Package orchestrator holds the investigation projection, its transition table and the named commands that change it (PERD §28; REQ-STM-001).
//
// May import: platform/*, internal/contracts. It reaches Trust, Execution and Outcome only through interfaces defined in internal/contracts; cmd/core wires the implementations.
//
// Must never: import the internals of trust, execution or outcome, set a state by assigning a field, or decide, execute or verify anything itself.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package orchestrator
