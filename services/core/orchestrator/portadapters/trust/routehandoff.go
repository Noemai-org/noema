package trustport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
)

// RouteSessions reads the session an investigation recorded, by correlation, with the orchestrator's own login (investigations.Store).
type RouteSessions interface {
	SessionByCorrelation(ctx context.Context, tenantID, correlationID string) (investigations.RecordedSession, error)
}

// RouteRequestDecider is what a RouteRequest is handed to: the RouteDecider.
type RouteRequestDecider interface {
	DecideRoute(ctx context.Context, r RouteRequest) (RouteResult, error)
}

// RouteSessionHandoff is not built yet.
type RouteSessionHandoff struct {
	Sessions RouteSessions
	Next     RouteRequestDecider
}

// DecideRoute is not built yet: it passes the request on unchanged.
func (h RouteSessionHandoff) DecideRoute(ctx context.Context, r RouteRequest) (RouteResult, error) {
	if h.Next == nil {
		return RouteResult{}, errors.New("trustport: the handoff is not built")
	}
	return h.Next.DecideRoute(ctx, r)
}
