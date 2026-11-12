// Package orchestrator holds the investigation projection, its transition table and the named commands that change it (PERD §28; REQ-STM-001).
//
// May import: platform/*, internal/contracts. It reaches Trust, Execution and Outcome only through the ports in ports.go (interfaces that carry opaque references and contract enums; they live here because internal/contracts holds only generated types); cmd/core's compose.go wires the implementations.
//
// Must never: import the internals of trust, execution or outcome, set a state by assigning a field, or decide, execute or verify anything itself.
//
// STM-01 slice 1: the table (table.go), the machine (machine.go). G1 slice: the ports (ports.go) and the read-only runner (conversation.go). The projection table, the optimistic-lock store and the Record-backed recorder are the next slice.
package orchestrator
