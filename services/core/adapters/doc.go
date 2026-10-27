// Package adapters holds the enterprise adapters, one per registered capability or read surface; the only code that talks to an enterprise system. In the POC the target is the synthetic Tushe environment (SoT §14, §35).
//
// May import: platform/*, internal/contracts. It is imported only by execution and evidence.
//
// Must never: import trust, outcome or record, call a model or provider, exist for a PROHIBITED capability (BD-08), or infer a Tushe behaviour that has not been shown.
//
// Built so far (ENV-03, first slice): the interface of the logical service mock-case (case.go). The interfaces
// of the other logical services wait for the frozen Tushe OpenAPI and are not built (see README.md).
package adapters
