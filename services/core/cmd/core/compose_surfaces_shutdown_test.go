package main

// STM-01 surfaces, a shutdown during the startup pass and a missing Record reader (REQ-OPS-003, REQ-VER-004, REQ-STM-001; INV-12, INV-30, AGENT-16; A442, A476, A486, A537, interim,
// relayed, not yet committed): a signal that arrives while the recovery startup pass runs is a normal shutdown (exit 0, no fatal error); a pass that fails for another reason stays fatal.
// With no Record reader there is no Journal and the parts that need one are the named gap "record reader". Every tenant, ID and value is SYNTHETIC.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
)

// blockingReader holds the list until its context ends and returns that context's error, as a database call does when the process is told to stop.
type blockingReader struct{ entered chan struct{} }

func (r *blockingReader) Idle(ctx context.Context, _ string, _ int64, _ int) ([]recovery.Candidate, error) {
	close(r.entered)
	<-ctx.Done()
	return nil, fmt.Errorf("synthetic list: %w", ctx.Err())
}

// A signal during the pass cancels it and is not a fatal pass failure: nothing is delivered on Fatal, the pass ends, and /readyz is not ready.
func TestASignalDuringTheStartupPassIsNotAFatalError(t *testing.T) {
	rd := &blockingReader{entered: make(chan struct{})}
	s, _, err, _ := sfBuildRecovery(t, sfRecoveryEnv(), SurfaceSeams{Recovery: rd}, "ten_aa")
	if s == nil || err != nil {
		t.Fatalf("surface %v, error %v", s, err)
	}
	ctx, stop := context.WithCancel(t.Context())
	s.Start(ctx)
	<-rd.entered
	stop()
	sfDone(t, s)
	select {
	case e := <-s.Fatal():
		t.Fatalf("a signal during the pass was delivered as a fatal error: %v", e)
	default:
	}
	if ok, _ := s.Ready(); ok {
		t.Error("ready after a pass that was cancelled")
	}
}

// The process exits 0 on a signal even when the fatal channel holds the cancelled pass's error at the same moment (the select must not choose between them at random).
func TestACancelledPassErrorOnTheFatalChannelDuringAShutdownExitsZero(t *testing.T) {
	for i := 0; i < 50; i++ {
		ctx, stop := context.WithCancel(t.Context())
		stop()
		fatal := make(chan error, 1)
		fatal <- fmt.Errorf("synthetic pass: %w", context.Canceled)
		srv := newSfServer()
		if code, out := sfServe(t, ctx, srv, fatal); code != 0 {
			t.Fatalf("round %d: exit %d, log %q; want 0", i, code, out)
		}
	}
}

// Another failure of the pass stays fatal even when it arrives after the signal began: only a cancelled context is a shutdown.
func TestAnotherPassFailureStaysFatalForTheExitCode(t *testing.T) {
	fatal := make(chan error, 1)
	fatal <- errors.New("synthetic: no database")
	if code, _ := sfServe(t, t.Context(), newSfServer(), fatal); code != 1 {
		t.Fatalf("exit %d; want 1", code)
	}
}

// With no Record reader the Journal is not built: recovery is the named gap "record reader" (and not also "write path not built"), the gateway names it too, and no runner exists.
func TestNoRecordReaderIsTheNamedGapForRecoveryAndNoJournalIsBuilt(t *testing.T) {
	c := full()
	c.Records = nil
	wp := sfWritePorts()
	wp.Journal = nil
	s, gaps, err := buildRecoverySurface(osEnv(sfRecoveryEnv()), c, SurfaceSeams{Recovery: &sfReader{}}, &orchestratorRecorder{}, wp, []string{"ten_aa"}, discardLog())
	if s != nil || err != nil || len(gaps) != 1 || gaps[0] != "recovery: record reader not built" {
		t.Fatalf("surface %v, gaps %v, error %v; want [recovery: record reader not built]", s, gaps, err)
	}
	if wp.Journal != nil {
		t.Fatal("a Journal was built with no Record reader")
	}
}

func TestNoRecordReaderIsTheNamedGapForTheGateway(t *testing.T) {
	e, _, rec := sfGatewayWorld(t)
	svc, gaps, err := sfBuildGateway(t, e, nil, rec, &fakeRunner{})
	if svc != nil || err != nil || len(gaps) != 1 || gaps[0] != "input gateway: record reader not built" {
		t.Fatalf("service %v, gaps %v, error %v; want [input gateway: record reader not built]", svc, gaps, err)
	}
}

// Through buildSurfaces: no Record reader, a write seam with no Journal: the Journal stays absent, so no runner, no gateway and no recovery; the gaps name the reader, and the process
// starts (no error).
func TestBuildSurfacesWithNoRecordReaderBuildsNoJournalAndNamesTheReader(t *testing.T) {
	e, _, rec := sfGatewayWorld(t)
	for k, v := range sfRecoveryEnv() {
		e[k] = v
	}
	e[EnvInvestigationRunTimeoutMS] = e[recovery.EnvRunTimeoutMS]
	c := full()
	c.Gateway, c.Records, c.Ports.Recorder = nil, nil, rec
	var seen orchestrator.WritePorts
	seams := SurfaceSeams{Recovery: &sfReader{}, Write: func(Components) orchestrator.WritePorts {
		seen = sfWritePorts()
		seen.Journal = nil
		return seen
	}}
	c, s, closer, err := sfSurfaces(t, e, c, seams)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if c.Gateway != nil || s.Gateway != nil || s.Recovery != nil || c.Events != nil {
		t.Error("a surface was built that needs the Record reader")
	}
	if got := strings.Join(s.Gaps, "|"); got != "input gateway: record reader not built|recovery: record reader not built" {
		t.Errorf("gaps %q", got)
	}
	if ok, reason := composeReadiness(c, okAPI)(); ok || !strings.Contains(reason, "record reader") {
		t.Errorf("ready %v, reason %q; want the record reader named", ok, reason)
	}
}
