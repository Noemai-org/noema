package telemetry

// trace.go: the W3C trace context of one request (REC-02; PERD REQ-AUD-004, REQ-OBS-002; SoT PLAT-NFR-15). A trace context is two random identifiers and a flag. It carries
// no identity, no tenant and no customer data, and nothing here reads a clock: the identifiers are drawn from the entropy source the Tracer was built with.

import (
	"encoding/hex"
	"errors"
	"io"
)

// TraceContext is one position in a trace: the 16-byte trace identifier, the 8-byte span identifier of this position, and the sampled flag.
type TraceContext struct {
	TraceID [16]byte
	SpanID  [8]byte
	Sampled bool
}

// ErrNoEntropy is returned when the entropy source cannot give identifiers, or gives an all-zero one (which W3C forbids).
var ErrNoEntropy = errors.New("telemetry: no trace identifier could be drawn")

// Tracer mints trace contexts from an entropy source (crypto/rand in production, a fixed reader in tests). It holds no other state.
type Tracer struct{ entropy io.Reader }

// NewTracer refuses a nil entropy source.
func NewTracer(entropy io.Reader) (*Tracer, error) {
	if entropy == nil {
		return nil, ErrNoEntropy
	}
	return &Tracer{entropy: entropy}, nil
}

func (t *Tracer) draw(b []byte) error {
	for tries := 0; tries < 4; tries++ { // an all-zero identifier is invalid: draw again, a bounded number of times
		if _, err := io.ReadFull(t.entropy, b); err != nil {
			return ErrNoEntropy
		}
		for _, x := range b {
			if x != 0 {
				return nil
			}
		}
	}
	return ErrNoEntropy
}

// Root starts a new trace: a fresh trace identifier and span identifier, sampled. A caller's traceparent is never an input (the edge's caller is untrusted).
func (t *Tracer) Root() (TraceContext, error) {
	var tc TraceContext
	if err := t.draw(tc.TraceID[:]); err != nil {
		return TraceContext{}, err
	}
	if err := t.draw(tc.SpanID[:]); err != nil {
		return TraceContext{}, err
	}
	tc.Sampled = true
	return tc, nil
}

// Child is a new span in the same trace: the trace identifier and the flag of the parent, a fresh span identifier.
func (t *Tracer) Child(parent TraceContext) (TraceContext, error) {
	child := parent
	if err := t.draw(child.SpanID[:]); err != nil {
		return TraceContext{}, err
	}
	return child, nil
}

// Traceparent formats the W3C header value: version 00, lower-case hex, 55 characters.
func (tc TraceContext) Traceparent() string {
	flags := "00"
	if tc.Sampled {
		flags = "01"
	}
	return "00-" + hex.EncodeToString(tc.TraceID[:]) + "-" + hex.EncodeToString(tc.SpanID[:]) + "-" + flags
}

// ParseTraceparent reads a version 00 value strictly: exactly 55 characters, lower-case hex, neither identifier all zero, flags 00 or 01. Anything else is not a trace context
// (ok is false); the caller never repairs it.
func ParseTraceparent(s string) (TraceContext, bool) {
	var tc TraceContext
	if len(s) != 55 || s[:3] != "00-" || s[35] != '-' || s[52] != '-' {
		return TraceContext{}, false
	}
	if !lowerHex(s[3:35]) || !lowerHex(s[36:52]) || !lowerHex(s[53:55]) {
		return TraceContext{}, false
	}
	if _, err := hex.Decode(tc.TraceID[:], []byte(s[3:35])); err != nil {
		return TraceContext{}, false
	}
	if _, err := hex.Decode(tc.SpanID[:], []byte(s[36:52])); err != nil {
		return TraceContext{}, false
	}
	if allZero(tc.TraceID[:]) || allZero(tc.SpanID[:]) {
		return TraceContext{}, false
	}
	switch s[53:55] {
	case "00":
	case "01":
		tc.Sampled = true
	default:
		return TraceContext{}, false
	}
	return tc, true
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
