package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	explainerport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/explainer"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// compose_surfaces.go is the surfaces lane of the final composition (STM-01; REQ-STM-001; INV-12, INV-30, AGENT-16; A537, interim, relayed, not yet committed): the Input Gateway, the
// investigation workers, the events route, recovery and the explainer port. It builds each from the merged adapters and names what it cannot build.

// SurfaceSeams are what the surfaces need from parts that other lanes or other tasks build. A nil seam is a named gap, never a stand-in.
type SurfaceSeams struct {
	// Write gives the write ports (Executor, Verifier, Bindings; the Journal is built here when it is nil) from the composed Components.
	Write func(Components) orchestrator.WritePorts
	// Recovery lists the idle investigations of a tenant as that tenant's own login.
	Recovery recovery.Reader
	// Explainer is the template explainer over its facts source and recorder.
	Explainer explainerport.Explainer
}

// Surfaces is what buildSurfaces made besides what it put into Components.
type Surfaces struct {
	Gateway  *gatewayService
	Recovery *recoverySurface
	// Gaps is every extra line /readyz names after the plain component names, in a fixed order, as fixed text with no value.
	Gaps []string
}

// buildSurfaces plugs the surfaces into Components, in this order: the explainer port from its seam, the events route, the write runner and the Input Gateway with its workers, and
// recovery. It returns the Components with what it built, what it could not build as gap lines (fixed text, never a value), and one function that closes what it opened, safe to call more
// than once and returned with an error too. A part whose settings are absent or whose adapters are not built is a gap; a setting that is given and invalid is the error (A469, A486). The
// Journal is built here, over the Record reader, when the write seam gives none; the Executor, the Verifier and the Bindings come only from the seam, never from a stand-in.
func buildSurfaces(ctx context.Context, env Env, clk clock.Clock, ids chain.IDSource, c Components, seams SurfaceSeams, logger *slog.Logger) (Components, *Surfaces, func(), error) {
	s := &Surfaces{}
	var closers []func()
	closeAll := closeInReverse(func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	})
	if seams.Explainer != nil {
		c.Ports.Explainer = explainerport.Port{Explainer: seams.Explainer}
	}
	handler, gaps, err := buildEventsSurface(env, clk, c.Records)
	if err != nil {
		return c, nil, closeAll, err
	}
	c.Events = handler
	s.Gaps = append(s.Gaps, gaps...)

	var wp orchestrator.WritePorts
	if seams.Write != nil {
		wp = seams.Write(c)
	}
	if wp.Journal == nil && c.Records != nil {
		wp.Journal = tenantJournal{dbs: c.Records}
	}
	runner, err := buildSurfaceRunner(c, wp)
	if err != nil {
		return c, nil, closeAll, err
	}
	var turns turnRunner // a nil *WriteRunner in the interface would not be nil: only a built runner is given
	if runner != nil {
		turns = runner
	}
	rec, _ := c.Ports.Recorder.(*orchestratorRecorder)
	svc, gaps, err := buildGatewaySurface(ctx, env, clk, ids, c.Records, rec, turns, surfaceRunFailed(logger))
	if err != nil {
		return c, nil, closeAll, err
	}
	if svc != nil {
		c.Gateway, s.Gateway = svc.Gateway, svc
		closers = append(closers, svc.Close)
	}
	s.Gaps = append(s.Gaps, gaps...)

	s.Recovery, gaps, err = buildRecoverySurface(env, c, seams, rec, wp, c.ServedTenants, logger)
	if err != nil {
		closeAll()
		return c, nil, closeAll, err
	}
	s.Gaps = append(s.Gaps, gaps...)
	return c, s, closeAll, nil
}

// withSurfaceReadiness is the /readyz check of inner plus the surfaces: their gap lines, the gateway until it is open and recovery until its startup pass has finished (A476). inner's own
// reason comes first. Every line is fixed text with no value.
func withSurfaceReadiness(inner readiness, s *Surfaces) readiness {
	return func() (bool, string) {
		ok, reason := inner()
		extra := append([]string(nil), s.Gaps...)
		if s.Gateway != nil {
			if gok, gr := s.Gateway.Ready(); !gok {
				extra = append(extra, gr)
			}
		}
		if s.Recovery != nil {
			if rok, rr := s.Recovery.Ready(); !rok {
				extra = append(extra, rr)
			}
		}
		if s.Recovery == nil && !hasGapOf(s.Gaps, "recovery") {
			extra = append(extra, "recovery") // recovery is part of the slice: with no setting of its own it is named plainly, as "worker" is
		}
		switch {
		case len(extra) == 0:
			return ok, reason
		case ok:
			return false, notConfigured(extra)
		}
		return false, reason + ", " + strings.Join(extra, ", ")
	}
}

// hasGapOf is true when a gap line is of the part, "<part>: ...".
func hasGapOf(gaps []string, part string) bool {
	for _, g := range gaps {
		if strings.HasPrefix(g, part+": ") {
			return true
		}
	}
	return false
}

// processCloser is the Close of the Process: it stops the worker, then recovery (cancelled and joined before any pool closes, A658), then closes the surfaces and the components (their
// pools). It runs once however often it is called, and it is safe with no worker and no recovery.
func processCloser(rw *RunningWorker, rec *recoverySurface, closeSurfaces, closeComponents func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			rw.Stop()
			rec.Stop()
			closeSurfaces()
			closeComponents()
		})
	}
}
