package main

// STM-01 surfaces, recovery (REQ-VER-004, REQ-STM-001; INV-03, INV-12, INV-30, AGENT-08, AGENT-16; A330, A469, A476, A486, A490, interim, relayed, not yet committed): the startup pass runs once
// per served tenant before /readyz turns green, on a runner that can never dispatch; with a setting absent or a part not built recovery is a named gap; an invalid setting is fatal. Every
// tenant, ID and value is SYNTHETIC.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
)

type sfIdle struct {
	tenant        string
	idleMS, limit int64
	hadDeadline   bool
}

type sfReader struct {
	mu    sync.Mutex
	calls []sfIdle
	cands map[string][]recovery.Candidate
	err   error
}

func (r *sfReader) Idle(ctx context.Context, tenantID string, idleMS int64, limit int) ([]recovery.Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := ctx.Deadline()
	r.calls = append(r.calls, sfIdle{tenantID, idleMS, int64(limit), ok})
	return r.cands[tenantID], r.err
}

func sfRecoveryEnv() envMap {
	return envMap{recovery.EnvIdleMS: "240000", recovery.EnvBatchSize: "50", recovery.EnvRunTimeoutMS: "30000"}
}

func sfBuildRecovery(t *testing.T, e envMap, seams SurfaceSeams, tenants ...string) (*recoverySurface, []string, error, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	s, gaps, err := buildRecoverySurface(osEnv(e), full(), seams, &orchestratorRecorder{}, sfWritePorts(), tenants, slog.New(slog.NewJSONHandler(&buf, nil)))
	return s, gaps, err, &buf
}

func sfDone(t *testing.T, s *recoverySurface) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the startup pass did not end")
	}
}

// None of the settings: recovery is a plain gap with no extra line, and no error.
func TestNoRecoverySettingIsNoRecoveryAndNoExtraLine(t *testing.T) {
	s, gaps, err, _ := sfBuildRecovery(t, envMap{}, SurfaceSeams{Recovery: &sfReader{}}, "ten_aa")
	if s != nil || len(gaps) != 0 || err != nil {
		t.Fatalf("surface %v, gaps %v, error %v; want none", s, gaps, err)
	}
}

// Each absent setting is named in fixed text; no error (A486).
func TestAnAbsentRecoverySettingIsANamedGap(t *testing.T) {
	for _, drop := range []string{recovery.EnvIdleMS, recovery.EnvBatchSize, recovery.EnvRunTimeoutMS} {
		e := sfRecoveryEnv()
		delete(e, drop)
		s, gaps, err, _ := sfBuildRecovery(t, e, SurfaceSeams{Recovery: &sfReader{}}, "ten_aa")
		if want := "recovery: " + drop + " missing"; s != nil || err != nil || len(gaps) != 1 || gaps[0] != want {
			t.Errorf("%s absent: surface %v, gaps %v, error %v; want [%q]", drop, s, gaps, err, want)
		}
	}
}

// The reader (the Postgres list of idle investigations) is not on main: with every setting given and no reader, recovery is a named gap.
func TestNoRecoveryReaderIsANamedGap(t *testing.T) {
	s, gaps, err, _ := sfBuildRecovery(t, sfRecoveryEnv(), SurfaceSeams{}, "ten_aa")
	if s != nil || err != nil || len(gaps) != 1 || gaps[0] != "recovery: reader not built" {
		t.Fatalf("surface %v, gaps %v, error %v; want [recovery: reader not built]", s, gaps, err)
	}
}

// With a reader and an incomplete runner (a port missing) recovery is a named gap too, not a half runner.
func TestAnIncompleteRunnerIsANamedGapForRecovery(t *testing.T) {
	c := full()
	c.Ports.Trust = nil
	var buf bytes.Buffer
	s, gaps, err := buildRecoverySurface(osEnv(sfRecoveryEnv()), c, SurfaceSeams{Recovery: &sfReader{}}, &orchestratorRecorder{}, sfWritePorts(), []string{"ten_aa"}, slog.New(slog.NewJSONHandler(&buf, nil)))
	if s != nil || err != nil || len(gaps) != 1 || gaps[0] != "recovery: write path not built" {
		t.Fatalf("surface %v, gaps %v, error %v; want [recovery: write path not built]", s, gaps, err)
	}
}

// An invalid setting is fatal and names itself, never the value; an idle age that does not exceed the run timeout is fatal and names both (A477).
func TestAnInvalidRecoverySettingIsFatal(t *testing.T) {
	for name, value := range map[string]string{recovery.EnvIdleMS: "soon", recovery.EnvBatchSize: "0", recovery.EnvRunTimeoutMS: "-1"} {
		for _, others := range []bool{true, false} {
			e := envMap{name: value}
			if others {
				e = sfRecoveryEnv()
				e[name] = value
			}
			s, gaps, err, _ := sfBuildRecovery(t, e, SurfaceSeams{Recovery: &sfReader{}}, "ten_aa")
			if s != nil || len(gaps) != 0 || err == nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), value) {
				t.Errorf("%s=%q (others %v): surface %v, gaps %v, error %v; want a fatal error naming the setting only", name, value, others, s, gaps, err)
			}
		}
	}
	e := sfRecoveryEnv()
	e[recovery.EnvIdleMS] = e[recovery.EnvRunTimeoutMS]
	if s, _, err, _ := sfBuildRecovery(t, e, SurfaceSeams{Recovery: &sfReader{}}, "ten_aa"); s != nil || err == nil || !strings.Contains(err.Error(), recovery.EnvIdleMS) || !strings.Contains(err.Error(), recovery.EnvRunTimeoutMS) {
		t.Errorf("idle equal to run: surface %v, error %v; want a fatal error naming both", s, err)
	}
}

// Recovery never dispatches: whatever Executor the write ports carry, the recovery runner's is NoDispatch, and the other ports are kept (INV-03, AGENT-08).
func TestTheRecoveryRunnerNeverDispatches(t *testing.T) {
	in := sfWritePorts()
	out := recoveryWritePorts(in)
	if _, ok := out.Executor.(recovery.NoDispatch); !ok {
		t.Fatalf("executor %T; want recovery.NoDispatch", out.Executor)
	}
	if out.Verifier != in.Verifier || out.Journal != in.Journal || out.Bindings != in.Bindings {
		t.Error("the other write ports were changed")
	}
	if _, ok := recoveryWritePorts(orchestrator.WritePorts{}).Executor.(recovery.NoDispatch); !ok {
		t.Error("with no executor given, the executor is still NoDispatch")
	}
}

// The stored session is read through the tenant's own store; a tenant with no store is an error and never another's (INV-12, A490).
func TestRecoverySessionsOfATenantWithNoStoreIsAnError(t *testing.T) {
	_, err := recoverySessions{rec: &orchestratorRecorder{}}.SessionOf(t.Context(), orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_1"}, "inv_AA11")
	if !errors.Is(err, ErrNoStoreForTenant) {
		t.Fatalf("error %v; want ErrNoStoreForTenant", err)
	}
	if _, err := (recoverySessions{}).SessionOf(t.Context(), orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_1"}, "inv_AA11"); err == nil {
		t.Fatal("no error without a Recorder")
	}
}

// The startup pass runs once per served tenant, with the configured idle age and batch, every list under a deadline; /readyz is not ready before it and ready after (A476).
func TestTheStartupPassRunsOncePerTenantAndReadinessWaitsForIt(t *testing.T) {
	rd := &sfReader{}
	s, gaps, err, _ := sfBuildRecovery(t, sfRecoveryEnv(), SurfaceSeams{Recovery: rd}, "ten_aa", "ten_bb")
	if s == nil || len(gaps) != 0 || err != nil {
		t.Fatalf("surface %v, gaps %v, error %v; want a surface", s, gaps, err)
	}
	if ok, reason := s.Ready(); ok || reason != "recovery: startup pass not finished" {
		t.Fatalf("before the pass: ready %v, reason %q", ok, reason)
	}
	s.Start(t.Context())
	sfDone(t, s)
	if ok, reason := s.Ready(); !ok || reason != "" {
		t.Errorf("after the pass: ready %v, reason %q", ok, reason)
	}
	want := []sfIdle{{"ten_aa", 240000, 50, true}, {"ten_bb", 240000, 50, true}}
	if len(rd.calls) != len(want) || rd.calls[0] != want[0] || rd.calls[1] != want[1] {
		t.Fatalf("lists %+v; want %+v", rd.calls, want)
	}
	s.Start(t.Context())
	sfDone(t, s)
	if len(rd.calls) != 2 {
		t.Errorf("a second Start ran the pass again: %d lists", len(rd.calls))
	}
}

// A list that cannot be read is fatal: the error is delivered, the process is never ready, and the pass ends.
func TestAStartupPassThatCannotListIsFatalAndNeverReady(t *testing.T) {
	s, _, err, _ := sfBuildRecovery(t, sfRecoveryEnv(), SurfaceSeams{Recovery: &sfReader{err: errors.New("synthetic: no database")}}, "ten_aa")
	if s == nil || err != nil {
		t.Fatalf("surface %v, error %v", s, err)
	}
	s.Start(t.Context())
	sfDone(t, s)
	select {
	case e := <-s.Fatal():
		if e == nil {
			t.Fatal("a nil error was delivered")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no fatal error was delivered")
	}
	if ok, _ := s.Ready(); ok {
		t.Error("ready after a pass that failed")
	}
}

// The sweep Report counts are logged: rows with no recorded session and rows refused, as counts only (A490, AGENT-13): no investigation, principal or tenant of a row.
func TestTheStartupPassLogsTheReportCountsAndNoRow(t *testing.T) {
	foreign := recovery.Candidate{Investigation: orchestrator.Investigation{ID: "inv_ZZ99"}, TenantID: "ten_bb", PrincipalRef: "usr_zz", CorrelationID: "corr_ZZ99"}
	rd := &sfReader{cands: map[string][]recovery.Candidate{"ten_aa": {foreign}}}
	s, _, err, buf := sfBuildRecovery(t, sfRecoveryEnv(), SurfaceSeams{Recovery: rd}, "ten_aa")
	if s == nil || err != nil {
		t.Fatalf("surface %v, error %v", s, err)
	}
	s.Start(t.Context())
	sfDone(t, s)
	line := buf.String()
	if !strings.Contains(line, `"refused":1`) || !strings.Contains(line, `"no_recorded_session":0`) || strings.Contains(line, "inv_ZZ99") || strings.Contains(line, "usr_zz") {
		t.Fatalf("log %q; want the counts refused 1 and no_recorded_session 0, and no row", line)
	}
}
