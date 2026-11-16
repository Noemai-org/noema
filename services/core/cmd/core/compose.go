package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
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
//   - Ports.Admissions: the Record read that confirms a turn's INPUT_RECEIVED event (A161; a read as the session's tenant, STM-01 slice 2);
//   - Records: the Record reader, one pool per tenant, each connecting as that tenant's record_read login (REC-03, DEBT-04); the reconstruction route is mounted only
//     with it;
//   - the protected /v1 handler is not a field: buildAPI makes it from the session settings of the environment (EDG-01 slice 3b, edge.Load) and compose takes it as an argument.
type Components struct {
	Ports   orchestrator.Ports
	Records reconstruct.TenantDBs
	// AccessLog is where the RECORD_ACCESSED lines of Record reads go (REQ-AUD-003). It is required whenever Records is set: a Record reader that logs nothing is a startup error.
	AccessLog *slog.Logger
}

// Env is the process's configuration source, injected so that tests do not touch the real environment. Nothing read through it has a default (AGENT-16).
type Env struct {
	Getenv   func(string) string
	ReadFile func(string) ([]byte, error)
	ReadDir  func(string) ([]os.DirEntry, error)
}

// App is the composed slice. Today it holds the runner; the HTTP handlers that sit behind the protected API are added in buildAPI as they are built.
type App struct {
	Runner *orchestrator.Runner
}

// rolesOpsAndAudit may read a reconstruction (REQ-AUD-003; ruling A20: audit reads only through the tenant-bound Record read functions).
var rolesOpsAndAudit = []session.Role{session.RoleOps, session.RoleAudit}

// apiRoutes is the route table of the public API: each route with the roles that may call it. A route exists only when the component behind it does.
func apiRoutes(c Components, ids session.IDs) []edge.Route {
	var routes []edge.Route
	if c.Records != nil {
		routes = append(routes, edge.Route{
			Pattern: reconstruct.Route,
			Roles:   rolesOpsAndAudit,
			Handler: &reconstruct.Handler{DBs: c.Records, Auth: reconstruct.SessionAuthorizer{}, IDs: ids, Log: c.AccessLog},
		})
	}
	return routes
}

// buildAPI makes the protected /v1 handler (EDG-01 slice 3b): the verified session, the role gate of each route (deny by default) and the identity-free body guard, then the
// route's handler. It returns nil and no error when none of the session settings is given, and the core then serves health only; with some of them, or any wrong, it returns
// an error and the process does not start. A Record reader without session settings is an error too: its route would be unreachable. Routes are mounted only for the
// components that exist.
func buildAPI(c Components, e Env, clk clock.Clock, ids session.IDs) (http.Handler, error) {
	if c.Records != nil && c.AccessLog == nil {
		return nil, errors.New("the Record reader is configured without an access logger: its reads would not be logged (REQ-AUD-003)")
	}
	routes := apiRoutes(c, ids)
	h, err := edge.Load(e.Getenv, e.ReadFile, clk, ids, routes)
	if err != nil {
		return nil, fmt.Errorf("session configuration: %w", err)
	}
	if h == nil && len(routes) > 0 {
		return nil, errors.New("the Record reader is configured but the session settings are not: its route would be unreachable")
	}
	return h, nil
}

// missing names the first absent component, or "" when none is.
func missing(c Components, api http.Handler) string {
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
	case c.Ports.Admissions == nil:
		return "admissions"
	case api == nil:
		return "edge chain"
	case c.Records == nil:
		return "record reader"
	}
	return ""
}

// compose builds the runner from the real transition table, or refuses when a component is missing.
func compose(c Components, api http.Handler) (*App, error) {
	if name := missing(c, api); name != "" {
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
func composeReadiness(c Components, api http.Handler) readiness {
	return func() (bool, string) {
		if name := missing(c, api); name != "" {
			return false, name + " is not configured"
		}
		return true, ""
	}
}
