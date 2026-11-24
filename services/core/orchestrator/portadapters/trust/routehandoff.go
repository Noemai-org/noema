package trustport

import (
	"context"
	"fmt"

	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// The orchestrator's hand-over of the recorded session to Trust (TRU-12; A625, A639, interim, relayed, not yet committed; REQ-TRU-010, REQ-SEC-004; INV-12, BD-05). The orchestrator reads
// the investigation by correlation with its own login and puts the principal and role on the route request as typed data; Trust never reads the investigation.

// RouteSessions reads the session an investigation recorded, by correlation (investigations.Store, over the orchestrator's login).
type RouteSessions interface {
	SessionByCorrelation(ctx context.Context, tenantID, correlationID string) (investigations.RecordedSession, error)
}

// RouteRequestDecider is what a RouteRequest is handed to: the RouteDecider.
type RouteRequestDecider interface {
	DecideRoute(ctx context.Context, r RouteRequest) (RouteResult, error)
}

// RouteSessionHandoff sets RouteRequest.Session from the investigation of the request's own correlation and asks Next. A Session the caller put on the request is replaced, never read
// (INV-12). With no investigation, another tenant's, several, or a row that records no session, the error is returned and Next is never asked: no scope, no grant.
type RouteSessionHandoff struct {
	Sessions RouteSessions
	Next     RouteRequestDecider
}

// DecideRoute hands the recorded session to Next.
func (h RouteSessionHandoff) DecideRoute(ctx context.Context, r RouteRequest) (RouteResult, error) {
	if h.Sessions == nil || h.Next == nil {
		return RouteResult{}, ErrConfig
	}
	if r.TenantID == "" || r.CorrelationID == "" {
		return RouteResult{}, ErrRouteRequest
	}
	rec, err := h.Sessions.SessionByCorrelation(ctx, r.TenantID, r.CorrelationID)
	if err != nil {
		return RouteResult{}, fmt.Errorf("trustport: read the recorded session: %w", err)
	}
	r.Session = intake.Session{TenantID: r.TenantID, PrincipalID: rec.PrincipalID, Role: rec.PrincipalRole}
	return h.Next.DecideRoute(ctx, r)
}
