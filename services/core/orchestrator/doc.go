// Package orchestrator holds the investigation projection, its transition table and the named commands that change it (PERD §28; REQ-STM-001).
//
// May import: platform/*, internal/contracts. It reaches Trust, Execution and Outcome only through interfaces defined in internal/contracts; cmd/core wires the implementations.
//
// Must never: import the internals of trust, execution or outcome, set a state by assigning a field, or decide, execute or verify anything itself.
//
// STM-01 slice 1: the table (table.go), the machine (machine.go). The projection table, the optimistic-lock store and the Record event are the next slice.
package orchestrator
