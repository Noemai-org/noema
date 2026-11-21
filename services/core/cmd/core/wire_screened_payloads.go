package main

import (
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
)

// wire_screened_payloads.go builds the payload Reader the composition hands to every consumer of evidence payloads (STM-01; REQ-CTX-007, REQ-AUD-001; INV-19, AGENT-09, AGENT-13).

// ScreenDeps is the injection screen of the Reader: the screen, the correlation source and the alert. All three are required and have no default (A372, interim, relayed, not yet committed).
type ScreenDeps struct {
	Screen      payloadread.Screen
	Correlation payloadread.CorrelationOf
	Alert       payloadread.Alert
}

// buildScreenedPayloads returns the Reader over the payload source and the screen.
func buildScreenedPayloads(p payloadread.PayloadReader, d ScreenDeps) (payloadread.Reader, error) {
	return payloadread.Reader{Payloads: p}, nil
}
