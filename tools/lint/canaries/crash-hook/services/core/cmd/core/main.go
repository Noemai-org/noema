package main

import "github.com/Noemai-org/noema/services/core/execution/ledger"

// Canary: production code installs a crash hook. Each of these must fire "crash-hook": a keyed literal, an assignment, a parenthesised assignment, a pointer to the field, and a positional literal.
func wire() *ledger.Ledger {
	hook := func(ledger.Point) error { return nil }
	l := &ledger.Ledger{Boundary: hook}
	l.Boundary = hook
	(l.Boundary) = hook
	p := &l.Boundary
	*p = hook
	_ = ledger.Ledger{hook}
	return l
}

func main() { _ = wire() }
