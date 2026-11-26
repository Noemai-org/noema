package main

// STM-01 surfaces, the Input Gateway and its runner (REQ-STM-001, REQ-INP-002, REQ-INP-003, REQ-API-001; INV-03, INV-12, INV-30, AGENT-13, AGENT-16; A330, A469, A486, interim, relayed,
// not yet committed). The gateway is built only over a complete write runner; with a setting absent or the runner not built it is a named gap (the process starts and /readyz names it
// in fixed text); a setting that is given and invalid is fatal and names itself, never its value. Every tenant, ID and value is SYNTHETIC.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

type sfWrite struct{}

func (sfWrite) Execute(context.Context, orchestrator.Session, string) (orchestrator.Receipt, error) {
	return orchestrator.Receipt{}, nil
}
func (sfWrite) Verify(context.Context, orchestrator.Session, string, string) (contracts.EnumsV1Verdict, error) {
	return "", nil
}
func (sfWrite) Progress(context.Context, orchestrator.Session, string, string) (orchestrator.Progress, error) {
	return orchestrator.Progress{}, nil
}
func (sfWrite) Bound(context.Context, orchestrator.Session, orchestrator.Binding) error { return nil }

func sfWritePorts() orchestrator.WritePorts {
	w := sfWrite{}
	return orchestrator.WritePorts{Executor: w, Verifier: w, Journal: w, Bindings: w}
}

// A complete set of ports builds the runner.
func TestACompleteSetOfPortsBuildsTheWriteRunner(t *testing.T) {
	r, err := buildSurfaceRunner(full(), sfWritePorts())
	if err != nil || r == nil {
		t.Fatalf("runner %v, error %v; want a runner", r, err)
	}
}

// Any one port missing is a gap, not an error and not a half runner: the runner is nil.
func TestAMissingPortMeansNoRunnerAndNoError(t *testing.T) {
	drops := map[string]func(*Components, *orchestrator.WritePorts){
		"interpreter": func(c *Components, _ *orchestrator.WritePorts) { c.Ports.Interpreter = nil },
		"evidence":    func(c *Components, _ *orchestrator.WritePorts) { c.Ports.Evidence = nil },
		"trust":       func(c *Components, _ *orchestrator.WritePorts) { c.Ports.Trust = nil },
		"explainer":   func(c *Components, _ *orchestrator.WritePorts) { c.Ports.Explainer = nil },
		"recorder":    func(c *Components, _ *orchestrator.WritePorts) { c.Ports.Recorder = nil },
		"admissions":  func(c *Components, _ *orchestrator.WritePorts) { c.Ports.Admissions = nil },
		"executor":    func(_ *Components, w *orchestrator.WritePorts) { w.Executor = nil },
		"verifier":    func(_ *Components, w *orchestrator.WritePorts) { w.Verifier = nil },
		"journal":     func(_ *Components, w *orchestrator.WritePorts) { w.Journal = nil },
		"bindings":    func(_ *Components, w *orchestrator.WritePorts) { w.Bindings = nil },
	}
	for name, drop := range drops {
		c, w := full(), sfWritePorts()
		drop(&c, &w)
		if r, err := buildSurfaceRunner(c, w); r != nil || err != nil {
			t.Errorf("%s missing: runner %v, error %v; want none and no error", name, r, err)
		}
	}
}

// The failure report carries the investigation and the class of the error, never the error's text, which can hold request data (AGENT-13).
func TestARunFailureIsReportedByClassAndNeverByText(t *testing.T) {
	var buf bytes.Buffer
	report := surfaceRunFailed(slog.New(slog.NewJSONHandler(&buf, nil)))
	report("inv_AA11", errors.New("synthetic secret text 1234567890123456"))
	line := buf.String()
	if !strings.Contains(line, "inv_AA11") || !strings.Contains(line, "errors.errorString") || strings.Contains(line, "secret") || strings.Contains(line, "1234567890") {
		t.Fatalf("log line %q; want the investigation and the error class only", line)
	}
}

func sfGatewayWorld(t *testing.T) (envMap, reconstruct.TenantDBs, *orchestratorRecorder) {
	t.Helper()
	g := newGwDirs(t, tenA)
	o := newOrchDirs(t, tenA)
	e := g.env()
	e[EnvOrchestratorDSNDir], e[EnvOrchestratorKeyDir], e[EnvOrchestratorKeyMaxActive], e[EnvReadTimeoutMS] = o.dsn, o.keys, "2", "2000"
	for k, v := range startEnv() {
		e[k] = v
	}
	records := recordsOf(t, e)
	clk := clock.NewManual(apiEpoch)
	rec, closer, err := buildRecorder(t.Context(), osEnv(e), clk, ids.New(clk, nil), records)
	if err != nil || rec == nil {
		t.Fatalf("recorder %v, error %v", rec, err)
	}
	t.Cleanup(closer)
	return e, records, rec
}

func sfBuildGateway(t *testing.T, e envMap, records reconstruct.TenantDBs, rec *orchestratorRecorder, runner turnRunner) (*gatewayService, []string, error) {
	t.Helper()
	clk := clock.NewManual(apiEpoch)
	return buildGatewaySurface(t.Context(), osEnv(e), clk, ids.New(clk, nil), records, rec, runner, (&failures{}).report)
}

// None of the gateway's settings: a plain gap (Components.Gateway nil names it), no extra line, no error.
func TestNoGatewaySettingIsAPlainGap(t *testing.T) {
	svc, gaps, err := sfBuildGateway(t, envMap{}, &failingDBs{}, nil, &fakeRunner{})
	if svc != nil || len(gaps) != 0 || err != nil {
		t.Fatalf("service %v, gaps %v, error %v; want none", svc, gaps, err)
	}
}

// Every setting given and a runner: the gateway is open and ready, and closing it makes it not ready.
func TestAFullConfigurationOverARunnerOpensTheGateway(t *testing.T) {
	e, records, rec := sfGatewayWorld(t)
	svc, gaps, err := sfBuildGateway(t, e, records, rec, &fakeRunner{})
	if err != nil || svc == nil || svc.Gateway == nil || len(gaps) != 0 {
		t.Fatalf("service %v, gaps %v, error %v; want an open gateway", svc, gaps, err)
	}
	if ok, reason := svc.Ready(); !ok || reason != "" {
		t.Errorf("ready %v, reason %q", ok, reason)
	}
	svc.Close()
	if ok, _ := svc.Ready(); ok {
		t.Error("ready after Close")
	}
}

// Every setting given and no runner (the write path is not built): a gap named in fixed text, no gateway, no error, and no worker was started.
func TestAFullConfigurationWithNoRunnerIsANamedGap(t *testing.T) {
	e, records, rec := sfGatewayWorld(t)
	svc, gaps, err := sfBuildGateway(t, e, records, rec, nil)
	if svc != nil || err != nil || len(gaps) != 1 || gaps[0] != "input gateway: write path not built" {
		t.Fatalf("service %v, gaps %v, error %v; want [input gateway: write path not built]", svc, gaps, err)
	}
}

// Each setting that is absent while the others are given is named, in fixed text; no gateway and no error (A486).
func TestAnAbsentGatewaySettingIsANamedGap(t *testing.T) {
	for _, drop := range []string{EnvEdgeDSNDir, EnvEdgeKeyDir, EnvEdgeKeyMaxActive, EnvInputHashKeyFile, EnvInputWindow, EnvInputRecordTimeout, edge.EnvMaxBody,
		EnvInvestigationWorkers, EnvInvestigationQueue, EnvInvestigationRunTimeoutMS, EnvInvestigationStartTimeoutMS} {
		e, records, rec := sfGatewayWorld(t)
		delete(e, drop)
		svc, gaps, err := sfBuildGateway(t, e, records, rec, &fakeRunner{})
		if want := "input gateway: " + drop + " missing"; svc != nil || err != nil || len(gaps) != 1 || gaps[0] != want {
			t.Errorf("%s absent: service %v, gaps %v, error %v; want [%q]", drop, svc, gaps, err, want)
		}
	}
}

// A setting that is given and invalid is fatal even while others are absent, and the error names the setting, never the value (A469).
func TestAnInvalidGatewaySettingIsFatalAndNamesItNotItsValue(t *testing.T) {
	bad := map[string]string{EnvInvestigationWorkers: "zero", EnvInvestigationQueue: "0", EnvInvestigationRunTimeoutMS: "-5", EnvInvestigationStartTimeoutMS: "1.5",
		EnvEdgeKeyMaxActive: "many", EnvInputWindow: "whenever", EnvInputRecordTimeout: "-3s", edge.EnvMaxBody: "huge"}
	for name, value := range bad {
		for _, absent := range []string{"", EnvInputHashKeyFile} {
			e, records, rec := sfGatewayWorld(t)
			e[name] = value
			if absent != "" {
				delete(e, absent)
			}
			svc, gaps, err := sfBuildGateway(t, e, records, rec, &fakeRunner{})
			if svc != nil || len(gaps) != 0 || err == nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), value) {
				t.Errorf("%s=%q (absent %q): service %v, gaps %v, error %v; want a fatal error naming the setting only", name, value, absent, svc, gaps, err)
			}
		}
	}
}
