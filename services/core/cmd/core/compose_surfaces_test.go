package main

// STM-01 surfaces, the composition (REQ-STM-001, REQ-API-001, REQ-INP-002, REQ-VER-004; INV-03, INV-12, INV-30, AGENT-16; A330, A442, A469, A486, A537, interim, relayed, not yet committed):
// buildSurfaces plugs the explainer port, the events route, the Input Gateway with its workers and recovery into Components, names every part it cannot build, and the process exits
// non-zero when a worker or a recovery pass dies. Every tenant, ID and value is SYNTHETIC.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/events"
	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	explainerport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/explainer"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

type sfExplainer struct{ asked []string }

func (e *sfExplainer) Explain(_ context.Context, s explanation.Subject, id string) (explanation.Explanation, error) {
	e.asked = append(e.asked, s.TenantID+"|"+id)
	return explanation.Explanation{Text: "A synthetic answer."}, nil
}

func sfSurfaces(t *testing.T, e envMap, c Components, seams SurfaceSeams) (Components, *Surfaces, func(), error) {
	t.Helper()
	clk := clock.NewManual(apiEpoch)
	return buildSurfaces(t.Context(), osEnv(e), clk, ids.New(clk, nil), c, seams, discardLog())
}

// The explainer seam becomes the orchestrator's Explainer port, and asks the explainer as the session's tenant; with no seam the port stays nil and /readyz names "explainer".
func TestTheExplainerSeamBecomesTheExplainerPort(t *testing.T) {
	x := &sfExplainer{}
	c, _, closer, err := sfSurfaces(t, envMap{}, Components{}, SurfaceSeams{Explainer: x})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	p, ok := c.Ports.Explainer.(explainerport.Port)
	if !ok || p.Explainer != x {
		t.Fatalf("explainer port %T; want explainerport.Port over the seam", c.Ports.Explainer)
	}
	c2, _, closer2, err := sfSurfaces(t, envMap{}, Components{}, SurfaceSeams{})
	if err != nil || c2.Ports.Explainer != nil {
		t.Fatalf("no seam: explainer %v, error %v; want nil", c2.Ports.Explainer, err)
	}
	closer2()
}

// With the Record reader and the eight settings the events handler is plugged in; with a setting absent it is a named gap in Surfaces.Gaps and Components.Events stays nil.
func TestTheEventsRouteIsPluggedInOrNamed(t *testing.T) {
	c, s, closer, err := sfSurfaces(t, sfEventsEnv(), Components{Records: &failingDBs{}}, SurfaceSeams{})
	if err != nil || c.Events == nil {
		t.Fatalf("events %v, error %v; want a handler", c.Events, err)
	}
	closer()
	if len(s.Gaps) != 0 {
		t.Errorf("gaps %v; want none", s.Gaps)
	}
	e := sfEventsEnv()
	delete(e, EnvEventsMaxBatch)
	c, s, closer, err = sfSurfaces(t, e, Components{Records: &failingDBs{}}, SurfaceSeams{})
	defer closer()
	if err != nil || c.Events != nil || len(s.Gaps) != 1 || s.Gaps[0] != "events route: "+EnvEventsMaxBatch+" missing" {
		t.Fatalf("events %v, gaps %v, error %v; want no handler and the one gap", c.Events, s.Gaps, err)
	}
}

// A setting that is given and invalid stops the composition, and nothing it opened stays open.
func TestAnInvalidSurfaceSettingStopsTheComposition(t *testing.T) {
	e := sfEventsEnv()
	e[EnvEventsPollMS] = "never"
	_, _, closer, err := sfSurfaces(t, e, Components{Records: &failingDBs{}}, SurfaceSeams{})
	if err == nil || !strings.Contains(err.Error(), EnvEventsPollMS) || closer == nil {
		t.Fatalf("error %v, closer %v; want a fatal error naming the setting and a callable closer", err, closer != nil)
	}
	closer()
}

// With the whole write path from the seam the gateway is plugged into Components and is ready, and its closer closes it; the Journal is built here when the seam gives none.
func TestTheGatewayIsPluggedInOverTheWritePathAndTheJournalIsBuilt(t *testing.T) {
	e, records, rec := sfGatewayWorld(t)
	c := full()
	c.Gateway, c.Records, c.Ports.Recorder = nil, records, rec
	var got orchestrator.WritePorts
	seams := SurfaceSeams{Write: func(Components) orchestrator.WritePorts {
		w := sfWritePorts()
		w.Journal = nil
		got = w
		return got
	}}
	c, s, closer, err := sfSurfaces(t, e, c, seams)
	if err != nil || c.Gateway == nil || s.Gateway == nil || len(s.Gaps) != 0 {
		t.Fatalf("gateway %v, surfaces %+v, error %v; want a gateway and no gap", c.Gateway, s, err)
	}
	if ok, reason := s.Gateway.Ready(); !ok || reason != "" {
		t.Errorf("ready %v, reason %q", ok, reason)
	}
	closer()
	if ok, _ := s.Gateway.Ready(); ok {
		t.Error("the gateway is ready after the closer ran")
	}
}

// With no write seam (the Execution Controller and Outcome adapters are not built) the gateway is a named gap, and no worker was started.
func TestNoWriteSeamIsAGatewayGapAndARecoveryGap(t *testing.T) {
	e, records, rec := sfGatewayWorld(t)
	for k, v := range sfRecoveryEnv() {
		e[k] = v
	}
	e[EnvInvestigationRunTimeoutMS] = e[recovery.EnvRunTimeoutMS]
	c := full()
	c.Gateway, c.Records, c.Ports.Recorder = nil, records, rec
	c, s, closer, err := sfSurfaces(t, e, c, SurfaceSeams{Recovery: &sfReader{}})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if c.Gateway != nil || s.Gateway != nil {
		t.Error("a gateway with no write path")
	}
	if got := strings.Join(s.Gaps, "|"); got != "input gateway: write path not built|recovery: write path not built" {
		t.Errorf("gaps %q", got)
	}
}

// The events route is mounted only with the handler, for customer sessions only (the role gate, INV-12).
func TestTheEventsRouteIsMountedOnlyWithItsHandlerForCustomers(t *testing.T) {
	for _, r := range apiRoutes(Components{}, nil) {
		if r.Pattern == events.Route {
			t.Fatal("the events route is mounted with no handler")
		}
	}
	routes := apiRoutes(Components{Events: &events.Handler{}}, nil)
	if len(routes) != 1 || routes[0].Pattern != events.Route || len(routes[0].Roles) != 1 || routes[0].Roles[0] != rolesCustomer[0] {
		t.Fatalf("routes %+v; want the events route for customers", routes)
	}
}

// Through newProcess: /readyz names "events route" and every extra gap in fixed text, no value, and the process starts (A486).
func TestTheProcessNamesTheSurfaceGapsInReadyz(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	e := sessionEnv(t, iss)
	e[edge.EnvTenants] = "ten_aa"
	dir, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("record_read_aa")})
	e[EnvRecordReadDSNDir] = dir
	e[EnvEventsPollMS] = "1000"
	e[recovery.EnvIdleMS] = "240000"
	var sink strings.Builder
	p, err := mcProcess(t, e, composeDeps{}, &sink)
	if err != nil || p == nil || p.Handler == nil {
		t.Fatalf("process %v, error %v", p, err)
	}
	defer p.Close()
	code, body := mcReadyz(p)
	for _, want := range []string{"events route: " + EnvEventsMaxStreamMS + " missing", "recovery: " + recovery.EnvBatchSize + " missing", "recovery: " + recovery.EnvRunTimeoutMS + " missing"} {
		if code != http.StatusServiceUnavailable || !strings.Contains(body, want) {
			t.Errorf("status %d, body %s; want 503 naming %q", code, body, want)
		}
	}
	if strings.Contains(body, "240000") || strings.Contains(body, dir) {
		t.Errorf("/readyz carries a value: %s", body)
	}
}

type sfFatalConn struct {
	*mcConn
	ch chan error
}

func (c sfFatalConn) Fatal() <-chan error { return c.ch }
func (c sfFatalConn) Ready() bool         { return true }

// The process's fatal channels are the worker's and recovery's: with neither there is none, and a worker that dies on its own is delivered through the process.
func TestTheProcessFatalChannelsAreTheWorkersAndRecoverys(t *testing.T) {
	if got := (&Process{}).fatalChans(); len(got) != 0 {
		t.Fatalf("channels %v; want none for a process with no worker and no recovery", got)
	}
	var log []string
	ch := make(chan error, 1)
	d := mcWorkerDeps(&log, nil, true)
	d.WorkerDial = func(context.Context, *WorkerConfig) (Connection, error) {
		return sfFatalConn{&mcConn{log: &log}, ch}, nil
	}
	var sink strings.Builder
	p, err := mcProcess(t, mcWorkerEnv(), d, &sink)
	if err != nil || p.Worker == nil {
		t.Fatalf("process %v, error %v", p, err)
	}
	defer p.Close()
	got := p.fatalChans()
	if len(got) != 1 {
		t.Fatalf("channels %d; want the worker's", len(got))
	}
	ch <- errors.New("synthetic")
	select {
	case e := <-got[0]:
		if e == nil {
			t.Fatal("nil error")
		}
	default:
		t.Fatal("the worker's fatal error did not come through the process")
	}
}
