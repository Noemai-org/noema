// Package decide: The one place a Decision Record is built: exactly one of the seven outcomes, fully
// attributed, with typed ResolvedIntent and UnresolvedIntent constructors and the clarification builder
// (PERD §23, §22).
//
// Implements: REQ-TRU-001, REQ-TRU-002, REQ-TRU-007, REQ-TRU-010, REQ-TRU-011, REQ-AMB-001, REQ-AMB-004,
// REQ-AMB-005.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts,
// trust/{reconcile,authority,policy,clocks}, evidence (read-only).
//
// Must never: let any other package construct a DecisionRecord (the construct-decision-grant lint); let an
// unresolved intent become anything but CLARIFY or ESCALATE; take a label, option or question from a model;
// import execution, outcome or a model client.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package decide
