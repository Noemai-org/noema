package trustport

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
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

// RouteDecider is the worker's Trust for the route step (STM-04; REQ-EXE-006, REQ-TRU-002; INV-03, INV-12, AGENT-08; A335, A363: interim, relayed, not yet committed). It asks the
// intake's continuation entry to decide the route capability as its own action decision, with the case id as the fact, under the session and intent of the stored admission, and
// returns what Trust recorded: it decides nothing and never makes a grant (INV-01). Runnable is the intake's Runnable port over the pack's graph.
type RouteDecider struct {
	Trust    StepEvaluator
	Scopes   RouteScopes
	Runnable intake.Runnable
}

// DecideRoute asks Trust for the route step. The queue the worker planned is not sent: the owner queue is a param of the winning PERMIT rule, read by Trust (a mismatch with the
// worker's dispatch fails at the Controller's params binding). An incomplete request, a scope of another tenant, a scope with no principal or intent, and an ALLOW that is not
// exactly one grant with a decision ID are errors with no grant. Any outcome other than ALLOW is returned as it is, with no decision ID and no grant.
func (d RouteDecider) DecideRoute(ctx context.Context, r RouteRequest) (RouteResult, error) {
	if d.Trust == nil || d.Scopes == nil || d.Runnable == nil {
		return RouteResult{}, ErrConfig
	}
	if r.TenantID == "" || r.CorrelationID == "" || r.CaseID == "" || r.CapabilityID == "" {
		return RouteResult{}, ErrRouteRequest
	}
	sc, err := d.Scopes.ScopeFor(ctx, r.TenantID, r.CorrelationID)
	if err != nil {
		return RouteResult{}, fmt.Errorf("trustport: read the admitted scope: %w", err)
	}
	if sc.Session.TenantID != r.TenantID || sc.Session.PrincipalID == "" {
		return RouteResult{}, ErrSession
	}
	if sc.IntentRef == "" {
		return RouteResult{}, fmt.Errorf("%w: the admitted scope names no intent", ErrIntentRef)
	}
	res, err := d.Trust.EvaluateStep(ctx, intake.ContinuationRequest{
		Request: intake.Request{Session: sc.Session, CorrelationID: r.CorrelationID, IntentRef: sc.IntentRef, EvidenceSnapshotID: sc.EvidenceSnapshotID,
			EvidenceUnavailable: sc.EvidenceUnavailable},
		CapabilityID: r.CapabilityID,
		Facts:        map[string]string{"case_id": r.CaseID},
		Runnable:     d.Runnable,
	})
	if err != nil {
		return RouteResult{}, fmt.Errorf("trustport: evaluate the route step: %w", err)
	}
	outcome, err := OutcomeOf(res.Decision.Outcome())
	if err != nil {
		return RouteResult{}, err
	}
	if outcome != contracts.EnumsV1DecisionOutcomeALLOW {
		return RouteResult{Outcome: string(outcome)}, nil
	}
	if res.DecisionID == "" || len(res.GrantIDs) != 1 || res.GrantIDs[0] == "" {
		return RouteResult{}, fmt.Errorf("%w: an ALLOW for the route step must carry a decision and exactly one grant", ErrBadAnswer)
	}
	return RouteResult{Outcome: string(outcome), DecisionID: res.DecisionID, GrantIDs: []string{res.GrantIDs[0]}}, nil
}
