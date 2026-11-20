package main

// STM-01 final composition, slice 2 (A442, interim, relayed, not yet committed): main starts the Temporal worker through startWorker when it is configured, and a configured worker that
// cannot start stops the process (no silent degraded mode). A missing worker is named by /readyz. Worker liveness (RunningWorker.Ready, the Fatal channel) is on noema#794 and is the
// next commit once it merges (status/STM-01-main-composition.md). REQ-VER-004, REQ-EXE-006, INV-30, AGENT-16. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/workflows"
)

type mcWorker struct {
	log      *[]string
	startErr error
}

func (w *mcWorker) RegisterWorkflowWithOptions(_ any, o workflow.RegisterOptions) {
	*w.log = append(*w.log, "workflow:"+o.Name)
}
func (w *mcWorker) RegisterActivity(any) { *w.log = append(*w.log, "activities") }
func (w *mcWorker) Start() error         { *w.log = append(*w.log, "start"); return w.startErr }
func (w *mcWorker) Stop()                { *w.log = append(*w.log, "stop") }

type mcConn struct {
	log      *[]string
	startErr error
}

func (c *mcConn) NewWorker(q string) temporalWorker {
	*c.log = append(*c.log, "worker:"+q)
	return &mcWorker{log: c.log, startErr: c.startErr}
}
func (c *mcConn) Close() { *c.log = append(*c.log, "close") }

func mcWorkerDeps(log *[]string, startErr error, withActivities bool) composeDeps {
	d := composeDeps{WorkerDial: func(_ context.Context, c *WorkerConfig) (Connection, error) {
		*log = append(*log, "dial:"+c.Namespace)
		return &mcConn{log: log, startErr: startErr}, nil
	}}
	if withActivities {
		d.WorkerActivities = func() *workflows.EscalateActivities { return &workflows.EscalateActivities{} }
	}
	return d
}

func mcWorkerEnv() envMap {
	return envMap{EnvTemporalAddress: "temporal.synthetic:7233", EnvTemporalNamespace: "noema", EnvTemporalTaskQueue: "noema-core", EnvTemporalActivityTimeout: "30000", EnvTemporalTLSCA: "/synthetic/ca.pem", EnvInvestigationStartTimeout: "30000"}
}

func mcEnvOf(e envMap) Env { return Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir} }

func TestMainStartsTheConfiguredWorkerOnTheConfiguredQueueAndStopsItOnce(t *testing.T) {
	var log []string
	rw, err := buildWorker(t.Context(), mcEnvOf(mcWorkerEnv()), mcWorkerDeps(&log, nil, true))
	if err != nil || rw == nil {
		t.Fatalf("worker %v, error %v", rw, err)
	}
	rw.Stop()
	rw.Stop()
	if got, want := strings.Join(log, ","), "dial:noema,worker:noema-core,workflow:"+workflows.EscalateWorkflowName+",activities,start,stop,close"; got != want {
		t.Fatalf("events %q, want %q", got, want)
	}
}

func TestWithNoTemporalSettingThereIsNoWorkerAndNoDial(t *testing.T) {
	var log []string
	rw, err := buildWorker(t.Context(), mcEnvOf(envMap{}), mcWorkerDeps(&log, nil, true))
	if err != nil || rw != nil || len(log) != 0 {
		t.Fatalf("worker %v, error %v, events %v; want nothing", rw, err, log)
	}
}

// A442, A469: a configured worker that was asked to start and cannot is never a quiet degraded mode: a setting that is wrong stops the start before any dial and the error names the setting;
// a start that fails is ErrWorkerNotStarted. (A worker whose activities are not built is a named gap, not an error: main_compose_worker_gap_test.go.)
func TestAConfiguredWorkerThatCannotStartIsAnErrorAndDialsNothing(t *testing.T) {
	var log []string
	bad := mcWorkerEnv()
	bad[EnvTemporalActivityTimeout] = "0"
	if rw, err := buildWorker(t.Context(), mcEnvOf(bad), mcWorkerDeps(&log, nil, true)); rw != nil || err == nil || !strings.Contains(err.Error(), EnvTemporalActivityTimeout) || len(log) != 0 {
		t.Errorf("bad setting: worker %v, error %v, events %v; want an error naming %s", rw, err, log, EnvTemporalActivityTimeout)
	}
	log = nil
	rw, err := buildWorker(t.Context(), mcEnvOf(mcWorkerEnv()), mcWorkerDeps(&log, errors.New("synthetic start failure"), true))
	if rw != nil || !errors.Is(err, ErrWorkerNotStarted) || log[len(log)-1] != "close" {
		t.Errorf("start failure: worker %v, error %v, events %v; want ErrWorkerNotStarted and the connection closed", rw, err, log)
	}
	if err != nil && strings.Contains(err.Error(), "temporal.synthetic") {
		t.Errorf("the error carries the address: %v", err)
	}
}

func mcProcess(t *testing.T, e envMap, d composeDeps, sink *strings.Builder) (*Process, error) {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	return newProcess(t.Context(), mcEnvOf(e), clk, ids.New(clk, nil), sink, d)
}

func mcReadyz(p *Process) (int, string) {
	rec := httptest.NewRecorder()
	p.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return rec.Code, rec.Body.String()
}

// /readyz names "worker" while there is none and stops naming it once it runs; closing the process stops it.
func TestTheProcessReadinessNamesAMissingWorkerAndTheProcessStopsARunningOne(t *testing.T) {
	var sink strings.Builder
	var log []string
	p, err := mcProcess(t, envMap{}, mcWorkerDeps(&log, nil, true), &sink)
	if err != nil || p.Handler == nil {
		t.Fatalf("process %v, error %v", p, err)
	}
	if code, body := mcReadyz(p); code != http.StatusServiceUnavailable || !strings.Contains(body, "worker") {
		t.Fatalf("no worker: status %d, body %s; want 503 naming worker", code, body)
	}
	p.Close()
	p, err = mcProcess(t, mcWorkerEnv(), mcWorkerDeps(&log, nil, true), &sink)
	if err != nil || p.Worker == nil {
		t.Fatalf("process %v, error %v", p, err)
	}
	if _, body := mcReadyz(p); strings.Contains(body, "worker") {
		t.Fatalf("a worker runs but /readyz says %s", body)
	}
	p.Close()
	p.Close()
	if got := strings.Join(log, ","); !strings.HasSuffix(got, "start,stop,close") || strings.Count(got, "stop") != 1 {
		t.Fatalf("events %q; want the worker stopped once on close", got)
	}
}

// A configured worker that does not start stops the process composition, and a worker already started is stopped when a later part fails.
func TestAProcessWithAWorkerThatDoesNotStartIsNotComposed(t *testing.T) {
	var sink strings.Builder
	var log []string
	p, err := mcProcess(t, mcWorkerEnv(), mcWorkerDeps(&log, errors.New("synthetic start failure"), true), &sink)
	if !errors.Is(err, ErrWorkerNotStarted) || p == nil || p.Close == nil {
		t.Fatalf("error %v; want ErrWorkerNotStarted and a Process whose Close can be called", err)
	}
	p.Close()
}

// A composition that is otherwise complete is still not ready while no worker runs, and is ready with one (noema#794 adds its liveness).
func TestAFullCompositionIsNotReadyWithoutAWorker(t *testing.T) {
	if ok, reason := processReadiness(full(), okAPI, nil)(); ok || reason != wantReason("worker") {
		t.Errorf("no worker: ready %v, reason %q; want not ready and %q", ok, reason, wantReason("worker"))
	}
	if ok, reason := processReadiness(full(), okAPI, &RunningWorker{})(); !ok || reason != "" {
		t.Errorf("a worker runs: ready %v, reason %q; want ready", ok, reason)
	}
}

// The production seams: the real Temporal dial is plugged in, there are no worker activities yet (noema#812), and no pack version is invented for Trust (A333: it comes from the
// manifest-verified pack, noema#773).
func TestTheProductionSeamsHoldWhatIsMergedAndInventNothing(t *testing.T) {
	d := productionDeps()
	if d.WorkerDial == nil || d.WorkerActivities == nil || d.WorkerActivities() != nil {
		t.Errorf("worker seams: dial %v, activities source %v; want the dial and no activities yet", d.WorkerDial != nil, d.WorkerActivities != nil)
	}
	a, err := d.TrustAdapters(Env{}, nil, clock.NewManual(time.Unix(1_800_000_000, 0)))
	var u *UnbuiltAdaptersError
	if !errors.As(err, &u) || a.Intents != nil || a.Capabilities != nil || a.PackVersion != "" {
		t.Errorf("production Trust adapters %+v, error %v; want none built and every one named", a, err)
	}
}
