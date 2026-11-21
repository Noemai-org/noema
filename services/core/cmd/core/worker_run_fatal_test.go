package main

// STM-04 (A442, interim, relayed, not yet committed; REQ-OPS-003, INV-30, AGENT-07): the handle main holds forwards the connection's fatal error and readiness. main selects on
// Fatal() and exits non-zero; /readyz names `worker` while it is down. No silent degraded mode. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/workflows"
)

type fatalConn struct {
	fakeConn
	f *fatalState
}

func (c *fatalConn) Fatal() <-chan error { return c.f.Fatal() }
func (c *fatalConn) Ready() bool         { return c.f.Ready() }

func startedWithFatal(t *testing.T) (*RunningWorker, *fatalState) {
	t.Helper()
	var log []string
	f := newFatalState()
	dial := func(_ context.Context, _ *WorkerConfig) (Connection, error) {
		return &fatalConn{fakeConn: fakeConn{log: &log}, f: f}, nil
	}
	rw, err := startWorker(deadlineCtx(), cfgFor(), dial, &workflows.EscalateActivities{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return rw, f
}

func TestTheHandleDeliversTheSDKFatalErrorOnceAndReadinessFlips(t *testing.T) {
	rw, f := startedWithFatal(t)
	if !rw.Ready() {
		t.Fatal("a live worker must be ready")
	}
	if ok, _ := rw.Check(); !ok {
		t.Fatal("a live worker must pass the readiness check")
	}
	f.report(errSyntheticFatal)
	f.report(errSyntheticFatal)
	n := 0
	for {
		select {
		case err := <-rw.Fatal():
			if !errors.Is(err, errSyntheticFatal) {
				t.Errorf("got %v", err)
			}
			n++
			continue
		default:
		}
		break
	}
	if n != 1 {
		t.Errorf("delivered %d times, want 1", n)
	}
	if rw.Ready() {
		t.Error("a dead worker must not be ready")
	}
	ok, reason := rw.Check()
	if ok || !strings.Contains(reason, "worker") {
		t.Errorf("check = %v %q, want not ready naming worker", ok, reason)
	}
}

func TestAConnectionWithoutFatalNeverReportsOneButIsNotSilentlyReady(t *testing.T) {
	var log []string
	dial := func(_ context.Context, _ *WorkerConfig) (Connection, error) { return &fakeConn{log: &log}, nil }
	rw, err := startWorker(deadlineCtx(), cfgFor(), dial, &workflows.EscalateActivities{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-rw.Fatal():
		t.Fatal("no fatal error was reported")
	default:
	}
	if !rw.Ready() {
		t.Error("a started worker with no fatal hook is ready until stopped")
	}
}

func TestAStoppedWorkerIsNotReadyAndNilIsNotReady(t *testing.T) {
	rw, _ := startedWithFatal(t)
	rw.Stop()
	if rw.Ready() {
		t.Error("a stopped worker must not be ready")
	}
	var nilRW *RunningWorker
	if nilRW.Ready() {
		t.Error("no worker is not ready")
	}
	if ok, reason := nilRW.Check(); ok || !strings.Contains(reason, "worker") {
		t.Errorf("nil check = %v %q", ok, reason)
	}
	select {
	case <-nilRW.Fatal():
		t.Fatal("a nil handle never delivers")
	default:
	}
}

func TestWithWorkerReadinessNamesTheWorkerAndKeepsTheOtherReason(t *testing.T) {
	rw, f := startedWithFatal(t)
	base := func() (bool, string) { return true, "" }
	chk := withWorker(base, rw)
	if ok, _ := chk(); !ok {
		t.Fatal("must be ready")
	}
	f.report(errSyntheticFatal)
	if ok, reason := chk(); ok || !strings.Contains(reason, "worker") {
		t.Errorf("got %v %q", ok, reason)
	}
	other := withWorker(func() (bool, string) { return false, "records is not configured" }, rw)
	if _, reason := other(); !strings.Contains(reason, "records") {
		t.Errorf("the base reason must come first: %q", reason)
	}
}
