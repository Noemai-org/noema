package telemetry_test

// REC-02: the trace context (REQ-AUD-004, REQ-OBS-002, REQ-SEC-003). Synthetic identifiers only.

import (
	"bytes"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/platform/telemetry"
)

func fixedTracer(t *testing.T) *telemetry.Tracer {
	t.Helper()
	b := make([]byte, 0, 256)
	for i := 1; i <= 200; i++ {
		b = append(b, byte(i))
	}
	tr, err := telemetry.NewTracer(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// REQ-AUD-004: a root trace formats as W3C version 00: 55 characters, lower-case hex, sampled; a child keeps the trace id and changes the span id.
func TestRootAndChildFormatAsW3C(t *testing.T) {
	tr := fixedTracer(t)
	root, err := tr.Root()
	if err != nil {
		t.Fatal(err)
	}
	tp := root.Traceparent()
	if len(tp) != 55 || !strings.HasPrefix(tp, "00-") || !strings.HasSuffix(tp, "-01") || tp != strings.ToLower(tp) {
		t.Fatalf("not a W3C traceparent: %q", tp)
	}
	child, err := tr.Child(root)
	if err != nil {
		t.Fatal(err)
	}
	if child.TraceID != root.TraceID || child.SpanID == root.SpanID {
		t.Fatalf("a child keeps the trace id and has a new span id: %v %v", root, child)
	}
}

// REQ-SEC-003: what is not exactly version 00, lower-case hex, 55 characters, non-zero ids and flags 00 or 01 is not a trace context.
func TestParseRefusesWhatIsNotAStrictTraceparent(t *testing.T) {
	good := "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	if _, ok := telemetry.ParseTraceparent(good); !ok {
		t.Fatal("a good value was refused")
	}
	for name, bad := range map[string]string{
		"empty":            "",
		"upper case":       "00-0123456789ABCDEF0123456789abcdef-0123456789abcdef-01",
		"version ff":       "ff-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
		"version 01":       "01-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
		"zero trace":       "00-00000000000000000000000000000000-0123456789abcdef-01",
		"zero span":        "00-0123456789abcdef0123456789abcdef-0000000000000000-01",
		"flags 03":         "00-0123456789abcdef0123456789abcdef-0123456789abcdef-03",
		"extra field":      good + "-00",
		"short":            good[:54],
		"space":            " " + good[1:],
		"not hex":          "00-0123456789abcdef0123456789abcdeg-0123456789abcdef-01",
		"trailing newline": good[:54] + "\n",
	} {
		if _, ok := telemetry.ParseTraceparent(bad); ok {
			t.Errorf("%s was accepted: %q", name, bad)
		}
	}
}

// REQ-AUD-004 (property): whatever string parses, formatting it again gives the same string.
func TestParseRoundTrips(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}`).Draw(t, "s")
		if tc, ok := telemetry.ParseTraceparent(s); ok && tc.Traceparent() != s {
			t.Fatalf("round trip: %q -> %q", s, tc.Traceparent())
		}
	})
}

// A source with no entropy, or only zeros, gives no trace: never an all-zero identifier.
func TestTracerRefusesNoEntropyAndAllZero(t *testing.T) {
	if _, err := telemetry.NewTracer(nil); err == nil {
		t.Fatal("a nil entropy source was accepted")
	}
	tr, _ := telemetry.NewTracer(bytes.NewReader(make([]byte, 512)))
	if _, err := tr.Root(); err == nil {
		t.Fatal("an all-zero source gave a trace")
	}
	tr, _ = telemetry.NewTracer(bytes.NewReader([]byte{1, 2, 3}))
	if _, err := tr.Root(); err == nil {
		t.Fatal("a short source gave a trace")
	}
}
