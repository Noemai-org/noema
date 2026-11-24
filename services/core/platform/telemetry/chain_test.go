package telemetry_test

// REC-02: the correlation chain in the context, the bound logger, the outgoing headers and the edge middleware
// (REQ-AUD-004, REQ-OBS-002, REQ-SEC-003; AGENT-13). Synthetic values only.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/redaction"
	"github.com/Noemai-org/noema/services/core/platform/telemetry"
)

const goodCorr = "corr_0AB12CD34"

func TestMiddlewareStartsANewTraceAndNeverAdoptsTheCallers(t *testing.T) {
	tr := fixedTracer(t)
	sent := "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"
	var seen telemetry.TraceContext
	var ok bool
	h := tr.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen, ok = telemetry.TraceFrom(r.Context()) }))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("traceparent", sent)
	req.Header.Set("tracestate", "vendor=1")
	req.Header.Set(telemetry.CorrelationHeader, "corr_0123456789")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !ok {
		t.Fatal("no trace was started")
	}
	if seen.Traceparent() == sent || strings.Contains(seen.Traceparent(), "aaaaaaaa") {
		t.Fatalf("the caller's traceparent was adopted: %s", seen.Traceparent())
	}
	if telemetry.CorrelationFrom(req.Context()) != "" {
		t.Fatal("a caller's correlation header became the chain's")
	}
}

func TestWithCorrelationKeepsOnlyAnIDOfTheEdgesShape(t *testing.T) {
	for _, bad := range []string{"", "x", "corr_", "corr_0123456789012345678901234567890123", "CORR_AB", "corr_a b", "corr_a\nb"} {
		if got := telemetry.CorrelationFrom(telemetry.WithCorrelation(context.Background(), bad)); got != "" {
			t.Errorf("%q was kept as %q", bad, got)
		}
	}
	if got := telemetry.CorrelationFrom(telemetry.WithCorrelation(context.Background(), goodCorr)); got != goodCorr {
		t.Fatalf("a good id was not kept: %q", got)
	}
}

func TestInjectSetsBothHeadersWithAChildSpan(t *testing.T) {
	tr := fixedTracer(t)
	root, _ := tr.Root()
	ctx := telemetry.WithCorrelation(telemetry.WithTrace(context.Background(), tr, root), goodCorr)
	h := http.Header{}
	telemetry.Inject(ctx, h)
	if h.Get(telemetry.CorrelationHeader) != goodCorr {
		t.Fatalf("correlation header: %q", h.Get(telemetry.CorrelationHeader))
	}
	got, ok := telemetry.ParseTraceparent(h.Get("traceparent"))
	if !ok || got.TraceID != root.TraceID || got.SpanID == root.SpanID {
		t.Fatalf("traceparent is not a child of the request's trace: %q", h.Get("traceparent"))
	}
	if h.Get("tracestate") != "" {
		t.Fatal("tracestate was sent")
	}
}

func TestInjectSetsNothingForAnEmptyChain(t *testing.T) {
	h := http.Header{}
	telemetry.Inject(context.Background(), h)
	if len(h) != 0 {
		t.Fatalf("headers were set without a chain: %v", h)
	}
}

// REQ-AUD-004: a line logged through the bound logger carries correlation_id and trace_id; REQ-SEC-003: through the redactor a 16-digit run in a field is masked.
func TestBoundLoggerCarriesTheChainAndPassesThroughTheRedactor(t *testing.T) {
	tr := fixedTracer(t)
	root, _ := tr.Root()
	ctx := telemetry.WithCorrelation(telemetry.WithTrace(context.Background(), tr, root), goodCorr)
	var buf bytes.Buffer
	base := slog.New(redaction.New().Handler(slog.NewJSONHandler(&buf, nil)))
	telemetry.Logger(ctx, base).Info("step done", "note", "account 1234567890123456 seen")
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("not JSON: %v: %s", err, buf.String())
	}
	if line["correlation_id"] != goodCorr || line["trace_id"] != root.TraceIDHex() {
		t.Fatalf("the chain is not on the line: %v", line)
	}
	if strings.Contains(buf.String(), "1234567890123456") {
		t.Fatalf("a 16-digit run reached the log: %s", buf.String())
	}
}

func TestLoggerWithoutAChainAddsNothing(t *testing.T) {
	var buf bytes.Buffer
	telemetry.Logger(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil))).Info("x")
	if strings.Contains(buf.String(), "correlation_id") || strings.Contains(buf.String(), "trace_id") {
		t.Fatalf("a field was invented: %s", buf.String())
	}
}
