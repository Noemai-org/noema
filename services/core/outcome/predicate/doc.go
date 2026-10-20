// Package predicate: Binding of outcome definitions to the predicate language of internal/predicate: load,
// type-check at pack load, evaluate.
//
// Implements: REQ-VER-002.
//
// May import: internal/predicate, internal/contracts, platform/config.
//
// Must never: define a second predicate language; evaluate with a different semantics than
// internal/predicate.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package predicate
