package main

// STM-01 surfaces, stopping the process (REQ-OPS-003, REQ-VER-004; INV-30; A658, interim, relayed, not yet committed, Codex P2): Stop cancels the recovery pass and joins it before any pool
// closes, and the Close of the Process runs once however often it is called. Every tenant and value is SYNTHETIC.

import (
	"context"
	"testing"
)

func sfClosed(s *recoverySurface) bool {
	select {
	case <-s.Done():
		return true
	default:
		return false
	}
}

// Stop cancels a pass that is still running and returns only when it has ended; the cancel by Stop is a normal shutdown, not a fatal error (REQ-VER-004).
func TestStopCancelsAndJoinsTheStartupPass(t *testing.T) {
	rd := &blockingReader{entered: make(chan struct{})}
	s, _, err, _ := sfBuildRecovery(t, sfRecoveryEnv(), SurfaceSeams{Recovery: rd}, "ten_aa")
	if s == nil || err != nil {
		t.Fatalf("surface %v, error %v", s, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.Start(ctx)
	<-rd.entered
	s.Stop()
	if !sfClosed(s) {
		t.Fatal("Stop returned while the startup pass was still running")
	}
	select {
	case e := <-s.Fatal():
		t.Fatalf("Stop was delivered as a fatal error: %v", e)
	default:
	}
	if ok, _ := s.Ready(); ok {
		t.Error("ready after a pass that was stopped")
	}
	s.Stop()
}

// Stop on a surface that never started returns, and a Stop after a pass that finished returns at once.
func TestStopOfASurfaceThatNeverStartedOrAlreadyEnded(t *testing.T) {
	s, _, err, _ := sfBuildRecovery(t, sfRecoveryEnv(), SurfaceSeams{Recovery: &sfReader{}}, "ten_aa")
	if s == nil || err != nil {
		t.Fatalf("surface %v, error %v", s, err)
	}
	s.Stop()
	s.Start(t.Context())
	sfDone(t, s)
	s.Stop()
	var none *recoverySurface
	none.Stop()
}

// The Close of the Process stops recovery before it closes the surfaces, and the surfaces before the components (their pools); a second call changes nothing (A658).
func TestTheProcessCloseJoinsRecoveryBeforeAnyPoolClosesAndRunsOnce(t *testing.T) {
	rd := &blockingReader{entered: make(chan struct{})}
	s, _, err, _ := sfBuildRecovery(t, sfRecoveryEnv(), SurfaceSeams{Recovery: rd}, "ten_aa")
	if s == nil || err != nil {
		t.Fatalf("surface %v, error %v", s, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.Start(ctx)
	<-rd.entered
	var order []string
	closeSurfaces := func() {
		if !sfClosed(s) {
			t.Error("the surfaces closed while the startup pass was still running")
		}
		order = append(order, "surfaces")
	}
	closeComponents := func() { order = append(order, "components") }
	closer := processCloser(nil, s, closeSurfaces, closeComponents)
	closer()
	closer()
	if len(order) != 2 || order[0] != "surfaces" || order[1] != "components" {
		t.Fatalf("closers ran %v; want [surfaces components] once", order)
	}
}

// With no worker and no recovery the Close still runs both closers once.
func TestTheProcessCloseWithNoWorkerAndNoRecovery(t *testing.T) {
	n := 0
	closer := processCloser(nil, nil, func() { n++ }, func() { n++ })
	closer()
	closer()
	if n != 2 {
		t.Fatalf("closers ran %d times; want 2 (once each)", n)
	}
}
