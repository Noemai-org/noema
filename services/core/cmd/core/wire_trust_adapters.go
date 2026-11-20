package main

import (
	"errors"
	"strings"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// EnvTrustEscalationGraph names the escalation graph of the pack (A319, A346). Required once a pack is given; no default (A330).
const EnvTrustEscalationGraph = "NOEMA_TRUST_ESCALATION_GRAPH"

// PackSource gives the manifest-verified pack (A333). Production has none until noema#773 is on main.
type PackSource func(env Env) (*trustpack.Pack, error)

var errPackNotOnMain = errors.New("cmd/core: the manifest-verified pack is not available")

func productionPack(Env) (*trustpack.Pack, error) { return nil, errPackNotOnMain }

// UnbuiltAdaptersError names every Trust adapter that could not be built.
type UnbuiltAdaptersError struct {
	Names []string
	Cause error
}

func (e *UnbuiltAdaptersError) Unwrap() error { return e.Cause }

func (e *UnbuiltAdaptersError) Error() string {
	return "cmd/core: the Trust service cannot start: not built: " + strings.Join(e.Names, ", ")
}

func trustAdaptersSource(pack PackSource) func(Env, reconstruct.TenantDBs, clock.Clock) (TrustAdapters, error) {
	return func(_ Env, _ reconstruct.TenantDBs, clk clock.Clock) (TrustAdapters, error) {
		return TrustAdapters{Clock: clk}, nil
	}
}
