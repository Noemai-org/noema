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
	ScopeFor(ctx context.Context, tenantID, correlationID string, session intake.Session) (RouteScope, error)
}

// RouteRequest is what the worker asks the route step with (A504, interim): the identity of the ask (tenant, correlation, capability) and the two parameters of the step, CaseID and
// OwnerQueue, both bound inside the route step's parameter hash (the pack's graph: case_id is a fact, owner_queue a policy param). VersionToken is not here: Execution reads it fresh at
// dispatch. CreateReceiptID is not here: the case ID comes from the verified create outcome. Another field, if one is ever needed, goes inside the hash with a test, never outside it.
// OwnerQueue is the queue the worker planned; it is not a fact for Trust, whose queue is the winning PERMIT rule's param, and a dispatch to another queue is refused by the Controller's
// params binding. The composition (cmd/core) converts the worker's RouteDecision into this field by field.
//
// Session is the one other field (A625, A639; interim, relayed, not yet committed): the principal and role the investigation recorded at its start, read by the orchestrator with its
// own login and handed to Trust as typed data (TRU-12). It is identity, not a parameter: it selects no capability, target or hash input, and Trust reads nothing of the investigation (BD-05).
type RouteRequest struct {
	TenantID, CorrelationID, CaseID, OwnerQueue, CapabilityID string
	Session                                                   intake.Session
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
	// Dispositions is the by-correlation read of the ESCALATE disposition, used when DecideRouteAfter is given none (A539). Optional: with none, an absent disposition stays absent.
	Dispositions Dispositions
}

// DecideRoute asks Trust for the route step. The queue the worker planned is not sent: the owner queue is a param of the winning PERMIT rule, read by Trust (a mismatch with the
// worker's dispatch fails at the Controller's params binding). An incomplete request, a scope of another tenant, a scope with no principal or intent, and an ALLOW that is not
// exactly one grant with a decision ID are errors with no grant. Any outcome other than ALLOW is returned as it is, with no decision ID and no grant.
func (d RouteDecider) DecideRoute(ctx context.Context, r RouteRequest) (RouteResult, error) {
	return d.DecideRouteAfter(ctx, r, "")
}

// DecideRouteAfter is DecideRoute for a route step that follows the ESCALATE disposition `dispositionDecisionID` (A534, A539), the one the workflow's input names. It is a separate
// argument and not a field of RouteRequest: the disposition is a link on the decision, not a parameter the step's hash binds, and the request holds only what the hash binds (A504).
// The intake alone decides whether it is recorded (A562: only a step of the escalation plan). With an empty id and a Dispositions port, the id is re-read from the Record by correlation
// (recovery, INV-30).
func (d RouteDecider) DecideRouteAfter(ctx context.Context, r RouteRequest, dispositionDecisionID string) (RouteResult, error) {
	if d.Trust == nil || d.Scopes == nil || d.Runnable == nil {
		return RouteResult{}, ErrConfig
	}
	if r.TenantID == "" || r.CorrelationID == "" || r.CaseID == "" || r.CapabilityID == "" {
		return RouteResult{}, ErrRouteRequest
	}
	sc, err := d.Scopes.ScopeFor(ctx, r.TenantID, r.CorrelationID, r.Session)
	if err != nil {
		return RouteResult{}, fmt.Errorf("trustport: read the admitted scope: %w", err)
	}
	if sc.Session.TenantID != r.TenantID || sc.Session.PrincipalID == "" {
		return RouteResult{}, ErrSession
	}
	if sc.IntentRef == "" {
		return RouteResult{}, fmt.Errorf("%w: the admitted scope names no intent", ErrIntentRef)
	}
	disposition, err := d.disposition(ctx, r, dispositionDecisionID)
	if err != nil {
		return RouteResult{}, err
	}
	res, err := d.Trust.EvaluateStep(ctx, intake.ContinuationRequest{
		Request: intake.Request{Session: sc.Session, CorrelationID: r.CorrelationID, IntentRef: sc.IntentRef, EvidenceSnapshotID: sc.EvidenceSnapshotID,
			EvidenceUnavailable: sc.EvidenceUnavailable},
		CapabilityID: r.CapabilityID,
		Facts:        map[string]string{"case_id": r.CaseID},
		Runnable:     d.Runnable,
		// The intake records it only on a step of the escalation plan (A562); it is not filtered again here.
		DispositionDecisionID: disposition,
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

// disposition is the ESCALATE disposition the route step follows: the one given, or, when none is given and a Dispositions port is set, the one the Record holds for the
// correlation (A539: recovery re-reads the Record, never workflow history; INV-30). A correlation with no disposition (ErrNoDisposition), a read that fails and a reader that says
// "found" with no id are errors: Trust is not asked and no grant comes back. With no port an absent disposition stays absent.
func (d RouteDecider) disposition(ctx context.Context, r RouteRequest, given string) (string, error) {
	if given != "" || d.Dispositions == nil {
		return given, nil
	}
	id, err := d.Dispositions.DispositionFor(ctx, r.TenantID, r.CorrelationID)
	if err != nil {
		return "", fmt.Errorf("trustport: read the ESCALATE disposition of the correlation: %w", err)
	}
	if id == "" {
		return "", fmt.Errorf("%w: the disposition read found a decision with no ID", ErrBadAnswer)
	}
	return id, nil
}
