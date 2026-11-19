// Package outbox is the transactional outbox of PERD §56 and §36.1: the one typed way to write an event in the transaction of the state change it belongs to
// (Enqueue), and the dispatcher that delivers pending events at least once and marks each dispatched only after its delivery (Dispatcher).
//
// Implements: REQ-VER-005 (the outbox part: "delivery is at least once, from the outbox"), PERD §56 ("Outbox"), EC-E16. Task PLT-08. SoT §35.3, INV-11, INV-12, INV-30.
//
// May import: the standard library, pgx and platform/clock.
//
// Must never: begin, commit or roll back the caller's transaction in Enqueue (an event written outside the state change's transaction is a dual write); import trust,
// execution, outcome or record; read the temporal database; carry a payload in an error text; read the wall clock (time comes through platform/clock).
package outbox
