package main

import (
	"context"
	"errors"
	"fmt"

	"go.temporal.io/sdk/client"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/workflows"
)

// worker_start.go: the START SITE of the escalation workflow (TRU-10; REQ-TRU-010, REQ-EXE-006; INV-12, INV-30, AGENT-08; A539, interim, relayed, not yet committed). It is the one place
// that starts EscalateCase in production, through the Temporal client of the worker's own package: workflow code does no I/O.

// workflowClient is the part of the Temporal client the start uses (client.Client has this method), so a test can record what is started.
type workflowClient interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow interface{}, args ...interface{}) (client.WorkflowRun, error)
}

// EscalationStart is what starts the escalation of one correlation. Session is the verified session (INV-12): the tenant of the case is its tenant and nothing else names one.
// DispositionDecisionID is the Result.DecisionID of the recorded ESCALATE.
type EscalationStart struct {
	Session                                                           orchestrator.Session
	CorrelationID, DispositionDecisionID, CreateGrantID, PlannedQueue string
}

// StartedEscalation names the workflow that runs the escalation.
type StartedEscalation struct{ WorkflowID, RunID string }

// EscalationStarter starts EscalateCase.
type EscalationStarter struct {
	c workflowClient
	w *WorkerConfig
}

// ErrEscalationStart is returned for a start that is not complete or not possible; the reason is in the text and never carries a server's words.
var ErrEscalationStart = errors.New("cmd/core: the escalation cannot be started")

// NewEscalationStarter builds the starter over a Temporal client and the worker's configuration (task queue and activity timeout, which have no default, AGENT-16).
func NewEscalationStarter(c workflowClient, w *WorkerConfig) (*EscalationStarter, error) {
	switch {
	case c == nil:
		return nil, fmt.Errorf("%w: no Temporal client", ErrEscalationStart)
	case w == nil || w.TaskQueue == "" || w.ActivityTimeout <= 0:
		return nil, fmt.Errorf("%w: no task queue or activity timeout", ErrEscalationStart)
	}
	return &EscalationStarter{c: c, w: w}, nil
}

// escalationWorkflowID is the one workflow id of a tenant's correlation: a second start of the same escalation names the same workflow and finds the first (idempotent start). It is
// never shared by two tenants or two correlations.
func escalationWorkflowID(tenantID, correlationID string) string {
	return "escalate_" + tenantID + "_" + correlationID
}

// Start starts the escalation of the correlation once. The tenant is the verified session's (INV-12). A session with no tenant or principal, an incomplete case and a context with no
// deadline are refused before the client is called. The client is asked with a deterministic workflow id and not to fail on a duplicate, so the SDK returns the run that exists; a
// workflow that already ran its create or route finds the grant spent and stops (AGENT-08). Workflow code does no I/O: the start is the only effect, and it is here (INV-30).
func (s *EscalationStarter) Start(ctx context.Context, r EscalationStart) (StartedEscalation, error) {
	if s == nil || s.c == nil || s.w == nil {
		return StartedEscalation{}, fmt.Errorf("%w: the starter is not built", ErrEscalationStart)
	}
	if _, ok := ctx.Deadline(); !ok {
		return StartedEscalation{}, fmt.Errorf("%w: the context needs a deadline", ErrEscalationStart)
	}
	if r.Session.TenantID == "" || r.Session.PrincipalID == "" {
		return StartedEscalation{}, fmt.Errorf("%w: the session has no tenant or no principal", ErrEscalationStart)
	}
	for name, v := range map[string]string{"correlation": r.CorrelationID, "disposition": r.DispositionDecisionID, "create grant": r.CreateGrantID, "queue": r.PlannedQueue} {
		if v == "" {
			return StartedEscalation{}, fmt.Errorf("%w: no %s", ErrEscalationStart, name)
		}
	}
	in := workflows.EscalateInput{TenantID: r.Session.TenantID, CorrelationID: r.CorrelationID, CreateGrantID: r.CreateGrantID, PlannedQueue: r.PlannedQueue,
		DispositionDecisionID: r.DispositionDecisionID, ActivityTimeout: s.w.ActivityTimeout}
	run, err := s.c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: escalationWorkflowID(r.Session.TenantID, r.CorrelationID), TaskQueue: s.w.TaskQueue},
		workflows.EscalateWorkflowName, in)
	if err != nil {
		return StartedEscalation{}, &startFailure{cause: err}
	}
	return StartedEscalation{WorkflowID: run.GetID(), RunID: run.GetRunID()}, nil
}

// startFailure is a start Temporal did not take. The text is fixed; the cause (which can name an address) stays reachable with errors.Is and errors.As (the pattern of startError).
type startFailure struct{ cause error }

func (e *startFailure) Error() string   { return ErrEscalationStart.Error() }
func (e *startFailure) Unwrap() []error { return []error{ErrEscalationStart, e.cause} }
