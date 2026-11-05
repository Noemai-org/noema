// Package planner: Deterministic plans from registered capability nodes only, with a canonical plan hash. A
// compound request is planned, never executed beyond the grant.
//
// Implements: REQ-PLN-001, REQ-PLN-002, REQ-PLN-003, REQ-AG-001.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, internal/predicate,
// execution/registry.
//
// Must never: plan an unregistered or PROHIBITED capability; choose beyond the grant; call a model; import
// trust or outcome.
//
// Built by PLN-01, slice 1: New builds a plan only from registry handles, and Hash is SHA-256 of the RFC 8785 form of the
// plan document. Slice 2: Walk chooses the steps by walking an Action Graph v2 (a step only after every node it depends on, ties by node ID,
// so the plan does not depend on the order of the document), and refuses a graph it cannot follow with certainty (a cycle, an unknown node, a
// root with an incoming edge, a conditional edge). Conditional edges, the choice of graph from disposition and classification, and
// PLAN_PRECONDITION_UNMET are not built.
package planner
