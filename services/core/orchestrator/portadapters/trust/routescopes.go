package trustport

import (
	"context"
	"errors"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// ErrNoRouteScope: the correlation has no sealed snapshot of the tenant, so there is no scope and no grant (A625).
var ErrNoRouteScope = errors.New("trustport: the correlation has no sealed snapshot to decide the route on")

// RouteScopeReader is not built yet.
type RouteScopeReader struct {
	logins  SnapshotLogins
	timeout time.Duration
}

var _ RouteScopes = RouteScopeReader{}

// NewRouteScopeReader is not built yet.
func NewRouteScopeReader(logins SnapshotLogins, timeout time.Duration) (RouteScopeReader, error) {
	return RouteScopeReader{logins: logins, timeout: timeout}, nil
}

// ScopeFor is not built yet.
func (r RouteScopeReader) ScopeFor(context.Context, string, string, intake.Session) (RouteScope, error) {
	return RouteScope{}, errors.New("trustport: ScopeFor is not built")
}
