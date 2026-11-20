package main

// STM-01 review fix (A469, A472, A475; interim, relayed, recorded in docs#216, not yet merged): the worker is a named gap, not a reason to stop the process, until it is configured; the start
// deadline is the required setting NOEMA_INVESTIGATION_START_TIMEOUT_MS and no constant. REQ-VER-004, REQ-EXE-006, INV-30, AGENT-16. Every value is SYNTHETIC.

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// The base Compose names Temporal by the address alone. That is not a worker configuration: the process starts, nothing is dialled and /readyz names the worker.
func TestAnAddressAloneIsNoWorkerConfigurationAndStartsTheProcess(t *testing.T) {
	var sink strings.Builder
	var log []string
	p, err := mcProcess(t, envMap{EnvTemporalAddress: "temporal.synthetic:7233"}, mcWorkerDeps(&log, nil, true), &sink)
	if err != nil || p == nil || p.Handler == nil || p.Worker != nil {
		t.Fatalf("process %v, error %v; want a process with no worker", p, err)
	}
	defer p.Close()
	if code, body := mcReadyz(p); code != http.StatusServiceUnavailable || !strings.Contains(body, "worker") || strings.Contains(body, "temporal.synthetic") {
		t.Errorf("status %d, body %s; want 503 naming worker and no address", code, body)
	}
	if len(log) != 0 {
		t.Errorf("events %v; want no dial", log)
	}
}

// A worker whose activities are not built (noema#812) is a gap too: configured and valid, still not started, nothing dialled.
func TestAConfiguredWorkerWhoseActivitiesAreNotBuiltIsAGap(t *testing.T) {
	var sink strings.Builder
	var log []string
	p, err := mcProcess(t, mcWorkerEnv(), mcWorkerDeps(&log, nil, false), &sink)
	if err != nil || p == nil || p.Handler == nil || p.Worker != nil {
		t.Fatalf("process %v, error %v; want a process with no worker", p, err)
	}
	defer p.Close()
	if _, body := mcReadyz(p); !strings.Contains(body, "worker") {
		t.Errorf("body %s; want /readyz to name worker", body)
	}
	if len(log) != 0 {
		t.Errorf("events %v; want no dial", log)
	}
}

// Once one worker setting beyond the address is given, a companion that is missing or wrong stays fatal, and the error names the setting and never the value.
func TestAConfiguredWorkerWithAMissingOrInvalidSettingIsFatalAndNamesIt(t *testing.T) {
	for _, drop := range []string{EnvTemporalAddress, EnvTemporalNamespace, EnvTemporalTaskQueue, EnvTemporalActivityTimeout, EnvTemporalTLSCA, EnvInvestigationStartTimeout} {
		e := mcWorkerEnv()
		delete(e, drop)
		var log []string
		rw, err := buildWorker(t.Context(), mcEnvOf(e), mcWorkerDeps(&log, nil, true))
		if rw != nil || err == nil || !strings.Contains(err.Error(), drop) || len(log) != 0 {
			t.Errorf("missing %s: worker %v, error %v, events %v; want an error naming it and no dial", drop, rw, err, log)
		}
	}
}

// A472: the start deadline is a required setting with no default. A bad value is fatal and named without its value; the setting is not asked for when no worker is configured.
func TestTheStartDeadlineIsARequiredSettingOfAConfiguredWorker(t *testing.T) {
	for _, bad := range []string{"0", "-1", "abc", "1.5", "99999999999999999999"} {
		e := mcWorkerEnv()
		e[EnvInvestigationStartTimeout] = bad
		var log []string
		rw, err := buildWorker(t.Context(), mcEnvOf(e), mcWorkerDeps(&log, nil, true))
		if rw != nil || err == nil || !strings.Contains(err.Error(), EnvInvestigationStartTimeout) || strings.Contains(err.Error(), bad) || len(log) != 0 {
			t.Errorf("value %q: worker %v, error %v, events %v; want an error naming %s without the value, and no dial", bad, rw, err, log, EnvInvestigationStartTimeout)
		}
	}
	for _, e := range []envMap{{}, {EnvTemporalAddress: "temporal.synthetic:7233"}} {
		var log []string
		if rw, err := buildWorker(t.Context(), mcEnvOf(e), mcWorkerDeps(&log, nil, true)); rw != nil || err != nil {
			t.Errorf("env %v: worker %v, error %v; want no worker and no error", e, rw, err)
		}
	}
}

// The setting is the bound: a dial that never answers ends when it passes (50 ms here), and the worker is not started.
func TestTheStartDeadlineBoundsTheDial(t *testing.T) {
	e := mcWorkerEnv()
	e[EnvInvestigationStartTimeout] = "50"
	d := mcWorkerDeps(new([]string), nil, true)
	d.WorkerDial = func(ctx context.Context, _ *WorkerConfig) (Connection, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		rw, err := buildWorker(t.Context(), mcEnvOf(e), d)
		if rw != nil {
			rw.Stop()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a dial that never answers started a worker")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the dial was not bounded by the start deadline setting")
	}
}

// A472: no code constant is the deadline. The old constant and its 30 s are gone from the non-test sources of cmd/core (the bench value lives in Compose only).
func TestNoConstantHoldsTheWorkerStartDeadline(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		n := en.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		if s := string(b); strings.Contains(s, "workerStartTimeout") || strings.Contains(s, "30 * time.Second") {
			t.Errorf("%s still holds a constant start deadline", n)
		}
	}
}
