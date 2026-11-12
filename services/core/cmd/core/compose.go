package main

import (
	"fmt"
	"net/http"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// compose.go is the composition root of the G1 slice (EDG-01 slice 3c). It is the only place that plugs the components other tasks build into the orchestrator,
// and the only file that does: the people who build a component give a constructor, and the Engineering Lead plugs it in here (the Architect's order of 11 Nov).
// It holds no product logic. A component that is missing means no composition and a /readyz that names it, so the process never serves half a slice.

// Components is everything the slice needs. Each field is built by another task and handed in:
//   - Ports.Interpreter: the model service client (UND), behind the input gateway;
//   - Ports.Evidence: the Evidence Layer over the stand-in read port (EVD-01, ENV-04, noema#441);
//   - Ports.Trust: the Trust gate and the Decision Record writer (TRU-05, noema#237);
//   - Ports.Explainer: the template explanation renderer (EXP-01);
//   - Ports.Recorder: the Record-backed recorder of state changes (STM-01 slice 2);
//   - Edge: the session middleware, the body guard and the role gate, in that order (EDG-01 slice 3b, noema#445).
type Components struct {
	Ports orchestrator.Ports
	Edge  func(http.Handler) http.Handler
}

// App is the composed slice. Today it holds the runner; the HTTP handlers that sit behind Edge are added here as they are built.
type App struct {
	Runner *orchestrator.Runner
}

// missing names the first absent component, or "" when none is.
func missing(c Components) string {
	switch {
	case c.Ports.Interpreter == nil:
		return "interpreter"
	case c.Ports.Evidence == nil:
		return "evidence"
	case c.Ports.Trust == nil:
		return "trust"
	case c.Ports.Explainer == nil:
		return "explainer"
	case c.Ports.Recorder == nil:
		return "recorder"
	case c.Edge == nil:
		return "edge chain"
	}
	return ""
}

// compose builds the runner from the real transition table, or refuses when a component is missing.
func compose(c Components) (*App, error) {
	if name := missing(c); name != "" {
		return nil, fmt.Errorf("compose: %s is not configured", name)
	}
	tab, err := orchestrator.LoadTable()
	if err != nil {
		return nil, fmt.Errorf("compose: the transition table: %w", err)
	}
	r, err := orchestrator.NewRunner(&orchestrator.Machine{Table: tab}, c.Ports)
	if err != nil {
		return nil, fmt.Errorf("compose: %w", err)
	}
	return &App{Runner: r}, nil
}

// composeReadiness is the /readyz check for a composition: ready only when compose would succeed, and otherwise it says which component is missing.
func composeReadiness(c Components) readiness {
	return func() (bool, string) {
		if name := missing(c); name != "" {
			return false, name + " is not configured"
		}
		return true, ""
	}
}
