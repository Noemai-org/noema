package main

// EDG-01 slice 3c: the composition root of the G1 slice (REQ-API-004, REQ-STM-001, INV-12). compose plugs the components that other tasks build into one runner and
// refuses to start with any of them missing, so /readyz says what is not configured instead of the process serving half a slice. The edge chain (session
// middleware, body guard, role gate) is required: a core with no authentication in front of it is never ready.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

type okPorts struct{}

func (okPorts) Interpret(context.Context, orchestrator.Session, orchestrator.Turn) (orchestrator.Interpretation, error) {
	return orchestrator.Interpretation{}, nil
}
func (okPorts) Read(context.Context, orchestrator.Session, string) (orchestrator.EvidenceRef, error) {
	return orchestrator.EvidenceRef{}, nil
}
func (okPorts) Decide(context.Context, orchestrator.Session, string, orchestrator.EvidenceRef) (orchestrator.Decision, error) {
	return orchestrator.Decision{}, nil
}
func (okPorts) Explain(context.Context, orchestrator.Session, string) (orchestrator.Explanation, error) {
	return orchestrator.Explanation{}, nil
}
func (okPorts) AppendChange(context.Context, orchestrator.Session, orchestrator.Change) error {
	return nil
}

func full() Components {
	p := okPorts{}
	return Components{
		Ports: orchestrator.Ports{Interpreter: p, Evidence: p, Trust: p, Explainer: p, Recorder: p},
		Edge:  func(next http.Handler) http.Handler { return next },
	}
}

func TestAFullyWiredSliceComposesAndIsReady(t *testing.T) {
	app, err := compose(full(), okAPI)
	if err != nil {
		t.Fatal(err)
	}
	if app.Runner == nil {
		t.Fatal("no runner")
	}
	if ok, reason := composeReadiness(full(), okAPI)(); !ok || reason != "" {
		t.Errorf("ready %v, reason %q; want ready", ok, reason)
	}
}

func TestAnyMissingComponentMeansNoCompositionAndNamesWhatIsMissing(t *testing.T) {
	cases := map[string]func(*Components){
		"interpreter": func(c *Components) { c.Ports.Interpreter = nil }, "evidence": func(c *Components) { c.Ports.Evidence = nil },
		"trust": func(c *Components) { c.Ports.Trust = nil }, "explainer": func(c *Components) { c.Ports.Explainer = nil },
		"recorder": func(c *Components) { c.Ports.Recorder = nil }, "edge chain": func(c *Components) { c.Edge = nil },
	}
	for name, drop := range cases {
		c := full()
		drop(&c)
		if app, err := compose(c, okAPI); err == nil || app != nil {
			t.Errorf("%s missing: composed (%v)", name, err)
		}
		ok, reason := composeReadiness(c, okAPI)()
		if ok || !strings.Contains(reason, name) {
			t.Errorf("%s missing: ready %v, reason %q; want not ready and the name in the reason", name, ok, reason)
		}
	}
}

// A composition without the protected /v1 handler (no session settings) is refused, and readiness names the edge chain.
func TestNoProtectedAPIMeansNoComposition(t *testing.T) {
	if app, err := compose(full(), nil); err == nil || app != nil {
		t.Fatalf("composed without an API: %v", err)
	}
	if ok, reason := composeReadiness(full(), nil)(); ok || !strings.Contains(reason, "edge chain") {
		t.Fatalf("ready %v, reason %q", ok, reason)
	}
}
