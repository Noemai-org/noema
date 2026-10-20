// Package corpus holds the tests that run shared, language-neutral corpora from /contracts against the core's own
// implementations. It exists so that those tests may read files, which the pure packages under test may not do.
//
// Implements REQ-VER-002 (the predicate corpus), REQ-RES-001 and REQ-RES-002 (the reconciliation corpus).
//
// May import: the standard library and the packages under test.
//
// Must never: be imported by production code.
package corpus
