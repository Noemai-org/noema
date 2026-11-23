package main

import (
	"context"
	"log/slog"

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

// buildSurfaces is not built yet.
func buildSurfaces(context.Context, Env, clock.Clock, chain.IDSource, Components, SurfaceSeams, *slog.Logger) (Components, *Surfaces, func(), error) {
	return Components{}, &Surfaces{}, func() {}, nil
}
