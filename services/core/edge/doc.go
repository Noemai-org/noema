// Package edge terminates client traffic: session authentication, rate limits, request and correlation IDs and server-sent events (PERD §64.2; REQ-API-004).
//
// May import: platform/*, internal/contracts, and the orchestrator through an interface defined in internal/contracts.
//
// Must never: decide, execute, verify, take identity from anything but the authenticated session (INV-12), or import the internals of trust, execution or outcome.
//
// Built so far: session verification, the identity-in-body guard and the role gate (package session, EDG-01 slices 1 to 3a), and their
// composition for the public API, Protected (slice 3b). Rate limits and server-sent events arrive with their tasks.
package edge
