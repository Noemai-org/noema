// Package workflows holds the Temporal workflows and activities: watches, confirmation expiry, recovery orchestration and callback delivery (SoT §35.6; PERD §36.1). Workflow code does no I/O; Temporal is never authoritative (INV-30).
//
// May import: internal/contracts and, in activities only, the packages they call through interfaces.
//
// Must never: do I/O in workflow code, read the wall clock, hold authoritative state, or import a model or provider client.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package workflows
