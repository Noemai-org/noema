// Package outcome independently verifies whether the intended outcome happened: the verifier, the predicate evaluator, durable watches and callbacks (SoT §13; PERD §29). It builds its own reads and never uses Trust's snapshot.
//
// May import: platform/*, internal/contracts and evidence (its own reads).
//
// Must never: import trust or execution internals, any model or provider client; execute, change a decision, use a receipt or an HTTP status as proof (INV-04), or use a float type.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package outcome
