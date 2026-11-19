package trustport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// ErrRouteRequest is returned for a route request that does not name what the decision needs.
var ErrRouteRequest = errors.New("trustport: the route request is incomplete")

// StepEvaluator is the intake's continuation entry (trust/intake.Service.EvaluateStep).
type StepEvaluator interface {
	EvaluateStep(ctx context.Context, r intake.ContinuationRequest) (intake.Result, error)
}

// RouteScope is what the route decision needs besides the worker's own request: the session and the intent the case was admitted under.
type RouteScope struct {
	Session             intake.Session
	IntentRef           string
	EvidenceSnapshotID  string
	EvidenceUnavailable bool
}

// RouteScopes finds the admitted session and intent of a correlation.
type RouteScopes interface {
	ScopeFor(ctx context.Context, tenantID, correlationID string) (RouteScope, error)
}

// RouteRequest has the same fields, in the same order, as the worker's RouteDecision (cmd/core), so that the composition converts one into the other.
type RouteRequest struct {
	TenantID, CorrelationID, CaseID, VersionToken, OwnerQueue, CreateReceiptID, CapabilityID string
}

// RouteResult has the same fields, in the same order, as the worker's RouteDecisionResult.
type RouteResult struct {
	Outcome    string
	DecisionID string
	GrantIDs   []string
}

// RouteDecider is the worker's Trust for the route step (not built: RED stub).
type RouteDecider struct {
	Trust    StepEvaluator
	Scopes   RouteScopes
	Runnable intake.Runnable
}

// DecideRoute is not built.
func (d RouteDecider) DecideRoute(ctx context.Context, r RouteRequest) (RouteResult, error) {
	return RouteResult{}, nil
}
