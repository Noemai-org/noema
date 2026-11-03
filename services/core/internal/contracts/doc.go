// Package contracts holds the Go types generated from /contracts by tools/codegen and committed here (CON-03; REQ-API-001,
// PERD §64.2). Nothing in a *_gen.go file is hand-written, and CI fails if regenerating changes any of them.
//
// A generated type decodes strictly: unknown members are refused where the schema is closed, required members must be present,
// a member that may not be null is not null, no member repeats, and an enum or constant holds one of its values. It does not
// check patterns, bounds or conditional rules; the JSON Schema validator does, at every boundary. Types is the registry used
// by contract tests to decode a fixture by name.
//
// May import: the standard library.
//
// Must never: contain a hand-edited type, or a benchmark, oracle or fault-plan type (tools/codegen/excluded.json).
package contracts
