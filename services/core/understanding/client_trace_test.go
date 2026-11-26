package understanding_test

// REC-02 (REQ-AUD-004, REQ-OBS-002): the compile call carries the chain of its request to the model service: Noema-Correlation-Id and a traceparent that is a child of the
// request's trace. A real HTTP server receives it; nothing is mocked. Synthetic values only.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/telemetry"
	"github.com/Noemai-org/noema/services/core/understanding"
)

func TestCompileCarriesTheCorrelationChainToTheModelService(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		http.Error(w, "x", http.StatusBadGateway)
	}))
	defer srv.Close()
	c, err := understanding.NewClient(understanding.ClientConfig{BaseURL: srv.URL, HTTP: srv.Client(), MaxResponseBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	entropy := bytes.NewReader(func() []byte {
		b := make([]byte, 200)
		for i := range b {
			b[i] = byte(i + 1)
		}
		return b
	}())
	tr, _ := telemetry.NewTracer(entropy)
	root, _ := tr.Root()
	ctx, cancel := context.WithTimeout(telemetry.WithCorrelation(telemetry.WithTrace(context.Background(), tr, root), "corr_0AB12CD34"), 5*time.Second)
	defer cancel()
	_, _ = c.Compile(ctx, understanding.CompileRequest{CanonicalInput: []byte(`{}`), PackRef: "fintech@1.0.0"})
	if got.Get("Noema-Correlation-Id") != "corr_0AB12CD34" {
		t.Fatalf("correlation header: %q", got.Get("Noema-Correlation-Id"))
	}
	child, ok := telemetry.ParseTraceparent(got.Get("traceparent"))
	if !ok || child.TraceID != root.TraceID || child.SpanID == root.SpanID {
		t.Fatalf("the call's traceparent is not a child of the request's trace: %q", got.Get("traceparent"))
	}
}

func TestCompileWithoutAChainSendsNoChainHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		http.Error(w, "x", http.StatusBadGateway)
	}))
	defer srv.Close()
	c, _ := understanding.NewClient(understanding.ClientConfig{BaseURL: srv.URL, HTTP: srv.Client(), MaxResponseBytes: 1 << 20})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = c.Compile(ctx, understanding.CompileRequest{CanonicalInput: []byte(`{}`), PackRef: "fintech@1.0.0"})
	if got.Get("Noema-Correlation-Id") != "" || got.Get("traceparent") != "" {
		t.Fatalf("headers without a chain: %v", got)
	}
}
