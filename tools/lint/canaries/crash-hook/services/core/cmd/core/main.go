package main

import "github.com/Noemai-org/noema/services/core/execution/ledger"

// Canary: production code installs a crash hook (an assignment, and a composite-literal key). Both must fire "crash-hook".
func wire() *ledger.Ledger {
	l := &ledger.Ledger{Boundary: func(ledger.Point) error { return nil }}
	l.Boundary = func(ledger.Point) error { return nil }
	return l
}

func main() { _ = wire() }
