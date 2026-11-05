// Package predicate: binding of outcome definitions to the predicate language of internal/predicate: load a pack's
// descriptors and variables and type-check every success predicate at load (OUT-01).
//
// Implements: REQ-VER-002.
//
// May import: internal/predicate, internal/contracts, platform/config.
//
// Must never: define a second predicate language; evaluate with a different semantics than internal/predicate.
package predicate
