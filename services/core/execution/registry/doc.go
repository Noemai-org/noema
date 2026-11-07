// Package registry: The Capability Registry: complete descriptors only; prohibited capabilities are
// registered, reject, and have no callable path; limits, timeouts and circuit breakers per capability.
//
// Implements: REQ-CAP-001, REQ-CAP-002, REQ-CAP-003.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts.
//
// Must never: load an incomplete descriptor; expose any route or adapter for a PROHIBITED capability
// (BD-08, INV-05); move money.
//
// Built by EXE-01. Load decodes a pack as data through the generated contract type and New refuses the whole set when
// a descriptor is incomplete, an ID repeats, or a monetary-write descriptor is not prohibited. Callable is the only
// way to obtain a handle, and it refuses every prohibited and unregistered ID with a typed error.
package registry
