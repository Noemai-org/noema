// Package outcomee2e holds the end-to-end tests of Outcome's verify-by-key against the real mock-case and real PostgreSQL
// (OUT-01; REQ-VER-001, REQ-RCV-002; US-06, US-16, EC-E03). It has no production code.
//
// It lives under adapters/ because the architecture lint lets only execution, evidence and adapters import adapters, and
// forbids outcome from reaching execution, in test files too (import-boundary, import-adapters). A test that dispatches
// through Execution, reads through the case adapter and verifies through Outcome can sit in no other area. The test-only
// control client of DEC-2026-10-26 Amendment 1 is in this package's _test.go files and nowhere else.
package outcomee2e
