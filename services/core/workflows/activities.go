package workflows

import (
	"context"
	"errors"
)

// GrantExecutor runs the spend-and-dispatch protocol for one grant and reports its outcome (execution/controller).
type GrantExecutor interface {
	ExecuteGrant(ctx context.Context, tenantID, grantID string) (GrantRun, error)
}

// CaseReader reads the case an idempotency key created, with its version token. It returns an error unless the
// answer is complete and names exactly one case: absence is never concluded from an incomplete answer.
type CaseReader interface {
	ReadCaseByKey(ctx context.Context, tenantID, idempotencyKey string) (CaseView, error)
}

// RouteGrantIssuer asks Trust for a grant for the route. Trust is not built in this slice; the test supplies a
// stand-in that signs with a synthetic key. The workflow never signs a grant itself (the four powers stay separate).
type RouteGrantIssuer interface {
	IssueRouteGrant(ctx context.Context, req RouteGrantRequest) (grantID string, err error)
}

// EscalateActivities holds the ports the activities call. Register it with a Temporal worker: its methods are the
// activities named in the Activities table.
type EscalateActivities struct {
	Executor GrantExecutor
	Reader   CaseReader
	Issuer   RouteGrantIssuer
}

// ExecuteGrant is the write activity of E1 to E11 for one grant. It never retries (D8).
func (a *EscalateActivities) ExecuteGrant(ctx context.Context, tenantID, grantID string) (GrantRun, error) {
	if a.Executor == nil {
		return GrantRun{}, errors.New("workflows: no executor")
	}
	return a.Executor.ExecuteGrant(ctx, tenantID, grantID)
}

// ReadBackCase is the read activity that finds the created case by the receipt's idempotency key.
func (a *EscalateActivities) ReadBackCase(ctx context.Context, tenantID, idempotencyKey string) (CaseView, error) {
	if a.Reader == nil {
		return CaseView{}, errors.New("workflows: no case reader")
	}
	return a.Reader.ReadCaseByKey(ctx, tenantID, idempotencyKey)
}

// IssueRouteGrant asks Trust for the route grant, bound to the version token that was read back.
func (a *EscalateActivities) IssueRouteGrant(ctx context.Context, req RouteGrantRequest) (string, error) {
	if a.Issuer == nil {
		return "", errors.New("workflows: no grant issuer")
	}
	return a.Issuer.IssueRouteGrant(ctx, req)
}
