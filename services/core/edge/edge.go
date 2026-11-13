package edge

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Noemai-org/noema/services/core/edge/session"
)

// Route is one endpoint of the public API and the roles that may call it.
type Route struct {
	Pattern string // a Go 1.22 mux pattern with a method and a path under /v1/, for example "POST /v1/cases"
	Roles   []session.Role
	Handler http.Handler
}

// Options is everything Protected needs. Nothing has a default (AGENT-16).
type Options struct {
	Verifier *session.Verifier
	IDs      session.IDs
	MaxBody  int64 // the largest request body read, in bytes
	Routes   []Route
}

// apiPrefix is the only path space the edge serves; health endpoints stay outside it and public.
const apiPrefix = "/v1/"

// Protected composes the public API's edge (REQ-API-004, ruling A1). Every request passes, in this order: the session check (401 without a valid
// session), the role gate of its route (404 for a role the route does not list), and the body guard (400 for a body that names an identity or
// exceeds MaxBody); only then does the route's handler run, with the principal in its context (INV-12). A path or method no route registers
// is denied by default: 401 without a session, 404 with one. A configuration that cannot be applied safely returns an error and no handler.
func Protected(o Options) (http.Handler, error) {
	if o.Verifier == nil || o.IDs == nil {
		return nil, errors.New("edge: a verifier and an ID source are required")
	}
	guard, err := session.RefuseBodyIdentity(o.IDs, o.MaxBody)
	if err != nil {
		return nil, fmt.Errorf("edge: %w", err)
	}
	authenticate := session.Middleware(o.Verifier, o.IDs)
	mux := http.NewServeMux()
	seen := make(map[string]bool, len(o.Routes))
	for _, r := range o.Routes {
		if err := checkRoute(r, seen); err != nil {
			return nil, err
		}
		if err := mount(mux, r, authenticate, guard, o.IDs); err != nil {
			return nil, err
		}
	}
	mux.Handle("/", authenticate(session.RequireRole(o.IDs)(http.NotFoundHandler()))) // no allowed role: deny by default
	return mux, nil
}

func checkRoute(r Route, seen map[string]bool) error {
	_, path, ok := strings.Cut(r.Pattern, " ")
	if !ok || !strings.HasPrefix(path, apiPrefix) || strings.ContainsAny(r.Pattern, "\t\n\r") {
		return fmt.Errorf("edge: route %q is not \"METHOD /v1/...\"", r.Pattern)
	}
	if seen[r.Pattern] {
		return fmt.Errorf("edge: route %q is registered twice", r.Pattern)
	}
	seen[r.Pattern] = true
	if r.Handler == nil || len(r.Roles) == 0 {
		return fmt.Errorf("edge: route %q needs a handler and at least one role", r.Pattern)
	}
	for _, role := range r.Roles {
		if _, ok := session.ParseRole(string(role)); !ok {
			return fmt.Errorf("edge: route %q names a role that does not exist", r.Pattern)
		}
	}
	return nil
}

// mount registers one route. ServeMux panics on a pattern it cannot parse or one that conflicts with an earlier one; that becomes an error here.
func mount(mux *http.ServeMux, r Route, authenticate, guard func(http.Handler) http.Handler, ids session.IDs) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("edge: route %q cannot be registered: %v", r.Pattern, p)
		}
	}()
	mux.Handle(r.Pattern, authenticate(session.RequireRole(ids, r.Roles...)(guard(r.Handler))))
	return nil
}
