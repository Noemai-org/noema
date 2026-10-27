package workflows

import (
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"
)

// Workflow names and statuses of the escalate case (graph fin.graph.escalate: create the case, then route it).
const (
	EscalateWorkflowName = "EscalateCase"

	// StatusAwaitingVerification: both writes were acknowledged. Execution never says succeeded; Outcome's verdict
	// (OUT-01, not built) is what turns an acknowledgement into ACHIEVED.
	StatusAwaitingVerification = "AWAITING_VERIFICATION"
	// StatusStopped: the case did not go on to the next write. Reason says why.
	StatusStopped = "STOPPED"
)

// Reasons for StatusStopped. After any of them nothing further is written: an unknown or refused write goes to
// verify-by-key and Trust, never to a Temporal retry (REQ-EXE-006, REQ-RCV-002).
const (
	ReasonCreateVoid            = "CREATE_GRANT_VOID"
	ReasonCreateReplay          = "CREATE_GRANT_ALREADY_SPENT"
	ReasonCreateNotAcknowledged = "CREATE_NOT_ACKNOWLEDGED"
	ReasonCaseNotReadable       = "CASE_NOT_READABLE_AFTER_CREATE"
	ReasonRouteGrantRefused     = "ROUTE_GRANT_NOT_ISSUED"
	ReasonRouteVoid             = "ROUTE_GRANT_VOID"
	ReasonRouteReplay           = "ROUTE_GRANT_ALREADY_SPENT"
	ReasonRouteNotAcknowledged  = "ROUTE_NOT_ACKNOWLEDGED"
)

// EscalateInput starts the case. CreateGrantID is a grant Trust already issued for the create. The timeouts come
// from configuration (REQ-OPS-003); the workflow refuses to start without them rather than choosing one.
type EscalateInput struct {
	TenantID      string
	CorrelationID string
	CreateGrantID string
	PlannedQueue  string
	// ActivityTimeout bounds one activity; it must exceed the dispatch timeout of the capability descriptor.
	ActivityTimeout time.Duration
}

// GrantRun is what one ExecuteGrant reports: the controller's outcome, without interpretation.
type GrantRun struct {
	GrantID        string
	Void           string // the void reason, when the grant was void
	Replayed       bool   // the grant was already spent; nothing was done
	Raced          bool   // another writer wrote this attempt's receipt first
	ReceiptID      string
	State          string // the Execution Receipt state
	TransportClass string
	RawCode        string
	ExternalAck    string
	IdempotencyKey string
	Attempt        int64
}

// Acknowledged is true only for a receipt the target acknowledged.
func (g GrantRun) Acknowledged() bool { return g.ReceiptID != "" && g.State == "ACKNOWLEDGED" }

// CaseView is the read-back of the created case: its identity and the version token the route is conditional on.
type CaseView struct {
	CaseID       string
	Reference    string
	Status       string
	OwnerQueue   string
	VersionToken string
}

// RouteGrantRequest is the ask to Trust for the route grant: the case and the version token read back.
type RouteGrantRequest struct {
	TenantID        string
	CorrelationID   string
	CaseID          string
	VersionToken    string
	PlannedQueue    string
	CreateReceiptID string
}

// EscalateResult is the workflow's report. It is a convenience: the authoritative state is in Postgres (INV-30).
type EscalateResult struct {
	Status       string
	Reason       string
	Create       GrantRun
	ReadBack     *CaseView
	RouteGrantID string
	Route        *GrantRun
}

// EscalateCase runs the escalate case: ExecuteGrant for the create, ReadBackCase by the receipt's idempotency
// key, IssueRouteGrant with the version token, ExecuteGrant for the route. It does no I/O: every effect is an
// activity (INV-30). It stops at the first write that is not acknowledged.
func EscalateCase(ctx workflow.Context, in EscalateInput) (EscalateResult, error) {
	if in.ActivityTimeout <= 0 || in.TenantID == "" || in.CreateGrantID == "" || in.PlannedQueue == "" {
		return EscalateResult{}, errors.New("workflows: EscalateCase needs a tenant, a create grant, a queue and an activity timeout")
	}
	var res EscalateResult
	create, err := runGrant(ctx, in, in.CreateGrantID)
	if err != nil {
		return res, err
	}
	res.Create = create
	if reason := stopReason(create, ReasonCreateVoid, ReasonCreateReplay, ReasonCreateNotAcknowledged); reason != "" {
		return stopped(res, reason), nil
	}

	var view CaseView
	if err := workflow.ExecuteActivity(readOptions(ctx, "ReadBackCase", in.ActivityTimeout), "ReadBackCase", in.TenantID, create.IdempotencyKey).Get(ctx, &view); err != nil {
		return stopped(res, ReasonCaseNotReadable), nil
	}
	res.ReadBack = &view

	var routeGrant string
	req := RouteGrantRequest{TenantID: in.TenantID, CorrelationID: in.CorrelationID, CaseID: view.CaseID, VersionToken: view.VersionToken,
		PlannedQueue: in.PlannedQueue, CreateReceiptID: create.ReceiptID}
	if err := workflow.ExecuteActivity(writeOptions(ctx, in.ActivityTimeout), "IssueRouteGrant", req).Get(ctx, &routeGrant); err != nil {
		return stopped(res, ReasonRouteGrantRefused), nil
	}
	res.RouteGrantID = routeGrant

	route, err := runGrant(ctx, in, routeGrant)
	if err != nil {
		return res, err
	}
	res.Route = &route
	if reason := stopReason(route, ReasonRouteVoid, ReasonRouteReplay, ReasonRouteNotAcknowledged); reason != "" {
		return stopped(res, reason), nil
	}
	res.Status = StatusAwaitingVerification
	return res, nil
}

func runGrant(ctx workflow.Context, in EscalateInput, grantID string) (GrantRun, error) {
	var run GrantRun
	if err := workflow.ExecuteActivity(writeOptions(ctx, in.ActivityTimeout), "ExecuteGrant", in.TenantID, grantID).Get(ctx, &run); err != nil {
		return GrantRun{GrantID: grantID}, fmt.Errorf("workflows: ExecuteGrant %s: %w", grantID, err)
	}
	return run, nil
}

func stopReason(g GrantRun, void, replay, notAcked string) string {
	switch {
	case g.Void != "":
		return void
	case g.Replayed:
		return replay
	case !g.Acknowledged():
		return notAcked
	}
	return ""
}

func stopped(res EscalateResult, reason string) EscalateResult {
	res.Status, res.Reason = StatusStopped, reason
	return res
}
