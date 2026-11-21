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

// A486 (interim, relayed, recorded in docs#216; it replaces the earlier reading that a companion missing from a configured worker is fatal): a worker setting that is missing is a gap,
// not a reason to stop. There is no worker, nothing is dialled, and /readyz names each missing setting by name and never a value.
func TestAMissingWorkerSettingIsAGapNamedByReadyzAndNotFatal(t *testing.T) {
	for _, drop := range []string{EnvTemporalAddress, EnvTemporalNamespace, EnvTemporalTaskQueue, EnvTemporalActivityTimeout, EnvTemporalTLSCA, EnvInvestigationStartTimeout} {
		e := mcWorkerEnv()
		delete(e, drop)
		var log []string
		rw, err := buildComposedWorker(t.Context(), mcEnvOf(e), mcWorkerDeps(&log, nil, true))
		if rw != nil || err != nil || len(log) != 0 {
			t.Errorf("missing %s: worker %v, error %v, events %v; want no worker, no error, no dial", drop, rw, err, log)
		}
		var sink strings.Builder
		p, err := mcProcess(t, e, mcWorkerDeps(&log, nil, true), &sink)
		if err != nil || p == nil || p.Handler == nil || p.Worker != nil {
			t.Fatalf("missing %s: process %v, error %v; want a process with no worker", drop, p, err)
		}
		code, body := mcReadyz(p)
		p.Close()
		if want := "worker: " + drop + " missing"; code != http.StatusServiceUnavailable || !strings.Contains(body, want) {
			t.Errorf("missing %s: status %d, body %s; want 503 naming %q", drop, code, body, want)
		}
		if strings.Contains(body, "temporal.synthetic") || strings.Contains(body, "/synthetic/") {
			t.Errorf("missing %s: /readyz carries a value: %s", drop, body)
		}
	}
}

// Every missing setting is named, not only the first, and a client certificate without its key (or the reverse) names the one that is missing.
func TestEveryMissingWorkerSettingIsNamed(t *testing.T) {
	var sink strings.Builder
	var log []string
	p, err := mcProcess(t, envMap{EnvTemporalAddress: "temporal.synthetic:7233", EnvTemporalTLSCert: "/synthetic/c.pem"}, mcWorkerDeps(&log, nil, true), &sink)
	if err != nil || p == nil || p.Worker != nil {
		t.Fatalf("process %v, error %v; want a process with no worker", p, err)
	}
	defer p.Close()
	_, body := mcReadyz(p)
	for _, n := range []string{EnvTemporalNamespace, EnvTemporalTaskQueue, EnvTemporalActivityTimeout, EnvTemporalTLSCA, EnvTemporalTLSKey, EnvInvestigationStartTimeout} {
		if want := "worker: " + n + " missing"; !strings.Contains(body, want) {
			t.Errorf("body %s; want %q", body, want)
		}
	}
	for _, n := range []string{EnvTemporalAddress, EnvTemporalTLSCert} {
		if strings.Contains(body, "worker: "+n+" missing") {
			t.Errorf("body %s; names %s, which is set", body, n)
		}
	}
	if len(log) != 0 {
		t.Errorf("events %v; want no dial", log)
	}
}

// A469 stands: a value that is present and invalid is fatal, even when another setting is missing, and the error names the setting and never the value.
func TestAPresentButInvalidWorkerSettingIsFatalEvenWithAMissingOne(t *testing.T) {
	cases := []struct{ name, value string }{
		{EnvTemporalAddress, "no-port-synthetic"},
		{EnvTemporalNamespace, "bad namespace synthetic"},
		{EnvTemporalTaskQueue, "bad queue/synthetic"},
		{EnvTemporalActivityTimeout, "abc-synthetic"},
		{EnvInvestigationStartTimeout, "0"},
		{EnvInvestigationRunTimeout, "-1"},
	}
	for _, c := range cases {
		e := envMap{EnvTemporalAddress: "temporal.synthetic:7233", c.name: c.value}
		var log []string
		rw, err := buildComposedWorker(t.Context(), mcEnvOf(e), mcWorkerDeps(&log, nil, true))
		if rw != nil || err == nil || !strings.Contains(err.Error(), c.name) || strings.Contains(err.Error(), c.value) || len(log) != 0 {
			t.Errorf("%s=%q with the others missing: worker %v, error %v, events %v; want a fatal error naming the setting without its value", c.name, c.value, rw, err, log)
		}
	}
}

// A477 (interim, relayed, recorded in docs#216): the activity timeout must be greater than the run timeout, or a live run could be picked up twice. Both present and activity <= run is
// refused, naming both settings and no value. One alone is no check.
func TestAnActivityTimeoutNotAboveTheRunTimeoutIsRefused(t *testing.T) {
	for _, c := range []struct {
		act, run string
		refused  bool
	}{{"30000", "30000", true}, {"29999", "30000", true}, {"1", "60000", true}, {"30001", "30000", false}, {"60000", "1", false}} {
		e := mcWorkerEnv()
		e[EnvTemporalActivityTimeout], e[EnvInvestigationRunTimeout] = c.act, c.run
		var log []string
		rw, err := buildComposedWorker(t.Context(), mcEnvOf(e), mcWorkerDeps(&log, nil, true))
		if c.refused {
			if rw != nil || err == nil || !strings.Contains(err.Error(), EnvTemporalActivityTimeout) || !strings.Contains(err.Error(), EnvInvestigationRunTimeout) || strings.Contains(err.Error(), c.act+" ") || len(log) != 0 {
				t.Errorf("activity %s, run %s: worker %v, error %v, events %v; want a refusal naming both settings and no dial", c.act, c.run, rw, err, log)
			}
			continue
		}
		if err != nil || rw == nil {
			t.Errorf("activity %s, run %s: worker %v, error %v; want a worker", c.act, c.run, rw, err)
		} else {
			rw.Stop()
		}
	}
	// The refusal holds when the worker is otherwise incomplete: both values are known.
	e := envMap{EnvTemporalActivityTimeout: "10", EnvInvestigationRunTimeout: "20"}
	if rw, err := buildComposedWorker(t.Context(), mcEnvOf(e), mcWorkerDeps(new([]string), nil, true)); rw != nil || err == nil {
		t.Errorf("incomplete worker, activity 10, run 20: worker %v, error %v; want a refusal", rw, err)
	}
	// The run timeout alone asks for no worker and no check.
	if rw, err := buildComposedWorker(t.Context(), mcEnvOf(envMap{EnvInvestigationRunTimeout: "20"}), mcWorkerDeps(new([]string), nil, true)); rw != nil || err != nil {
		t.Errorf("run timeout alone: worker %v, error %v; want a gap, not an error", rw, err)
	}
}

// A472: the start deadline is a required setting with no default. A bad value is fatal and named without its value; the setting is not asked for when no worker is configured.
func TestTheStartDeadlineIsARequiredSettingOfAConfiguredWorker(t *testing.T) {
	for _, bad := range []string{"0", "-1", "abc", "1.5", "99999999999999999999"} {
		e := mcWorkerEnv()
		e[EnvInvestigationStartTimeout] = bad
		var log []string
		rw, err := buildComposedWorker(t.Context(), mcEnvOf(e), mcWorkerDeps(&log, nil, true))
		if rw != nil || err == nil || !strings.Contains(err.Error(), EnvInvestigationStartTimeout) || strings.Contains(err.Error(), bad) || len(log) != 0 {
			t.Errorf("value %q: worker %v, error %v, events %v; want an error naming %s without the value, and no dial", bad, rw, err, log, EnvInvestigationStartTimeout)
		}
	}
	for _, e := range []envMap{{}, {EnvTemporalAddress: "temporal.synthetic:7233"}} {
		var log []string
		if rw, err := buildComposedWorker(t.Context(), mcEnvOf(e), mcWorkerDeps(&log, nil, true)); rw != nil || err != nil {
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
		rw, err := buildComposedWorker(t.Context(), mcEnvOf(e), d)
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

// A486: each worker setting given with the address alone asks for a worker that is incomplete: the process starts, there is no worker, and /readyz names what is missing.
func TestEachWorkerSettingBeyondTheAddressAloneIsAnIncompleteWorkerGap(t *testing.T) {
	full := mcWorkerEnv()
	full[EnvTemporalTLSCert], full[EnvTemporalTLSKey] = "/synthetic/c.pem", "/synthetic/k.pem"
	for _, n := range []string{EnvTemporalNamespace, EnvTemporalTaskQueue, EnvTemporalActivityTimeout, EnvTemporalTLSCert, EnvTemporalTLSKey, EnvTemporalTLSCA, EnvInvestigationStartTimeout} {
		e := envMap{EnvTemporalAddress: "temporal.synthetic:7233", n: full[n]}
		var log []string
		rw, err := buildComposedWorker(t.Context(), mcEnvOf(e), mcWorkerDeps(&log, nil, true))
		if rw != nil || err != nil || len(log) != 0 {
			t.Errorf("%s with the address: worker %v, error %v, events %v; want a gap, no error, no dial", n, rw, err, log)
		}
		if m := workerMissing(mcEnvOf(e)); len(m) == 0 {
			t.Errorf("%s with the address: no missing setting named", n)
		}
	}
}
