package main

import (
	"errors"
	"reflect"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
)

// wire_screened_payloads.go builds the payload Reader the composition hands to every consumer of evidence payloads (STM-01; REQ-CTX-007, REQ-AUD-001; INV-19, AGENT-09, AGENT-13). The
// Reader verifies a payload's hash before it decodes it into anything a mapping sees, then screens its text; a Reader without the screen refuses every call (payloadread, A372, interim,
// relayed, not yet committed). This file builds none of the parts: the production screen is not wired here yet (see status/STM-01-main-composition.md).

// ScreenDeps is the injection screen of the Reader: the screen, the correlation source and the alert. All three are required and have no default.
type ScreenDeps struct {
	Screen      payloadread.Screen
	Correlation payloadread.CorrelationOf
	Alert       payloadread.Alert
}

var errScreenedPayloads = errors.New("cmd/core: the payload reader needs a payload source, an injection screen, a correlation source and an alert")

// buildScreenedPayloads returns the Reader over the payload source and the screen. A missing part, or a nil pointer held in an interface (a nil *trustPayloads is what an unset
// NOEMA_TRUST_DSN_DIR gives), is an error: there is no Reader without the screen.
func buildScreenedPayloads(p payloadread.PayloadReader, d ScreenDeps) (payloadread.Reader, error) {
	if isNilValue(p) || isNilValue(d.Screen) || isNilValue(d.Alert) {
		return payloadread.Reader{}, errScreenedPayloads
	}
	r, err := payloadread.NewReader(p, d.Screen, d.Correlation, d.Alert)
	if err != nil {
		return payloadread.Reader{}, errScreenedPayloads
	}
	return r, nil
}

// isNilValue says whether an interface is nil or holds a nil pointer.
func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}
