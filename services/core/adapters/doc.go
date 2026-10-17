// Package adapters holds the enterprise adapters, one per registered capability or read surface; the only code that talks to an enterprise system. In the POC the target is the synthetic Tushe environment (SoT §14, §35).
//
// May import: platform/*, internal/contracts. It is imported only by execution and evidence.
//
// Must never: import trust, outcome or record, call a model or provider, exist for a PROHIBITED capability (BD-08), or infer a Tushe behaviour that has not been shown.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package adapters
