// Package edge terminates client traffic: session authentication, rate limits, request and correlation IDs and server-sent events (PERD §64.2; REQ-API-004).
//
// May import: platform/*, internal/contracts, and the orchestrator through an interface defined in internal/contracts.
//
// Must never: decide, execute, verify, take identity from anything but the authenticated session (INV-12), or import the internals of trust, execution or outcome.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package edge
