package main

import (
	"context"
	"errors"

	"go.temporal.io/sdk/client"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// worker_start.go: the START SITE of the escalation workflow (TRU-10; REQ-TRU-010, REQ-EXE-006; INV-12, INV-30, AGENT-08; A539, interim, relayed, not yet committed). It is the one place
// that starts EscalateCase in production, through the Temporal client of the worker's own package: workflow code does no I/O. Not built yet.

var errStartNotBuilt = errors.New("cmd/core: the escalation start is not built")

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

// NewEscalationStarter builds the starter over a Temporal client and the worker's configuration (task queue and activity timeout, which have no default).
func NewEscalationStarter(c workflowClient, w *WorkerConfig) (*EscalationStarter, error) {
	return &EscalationStarter{}, nil
}

// Start starts the escalation of the correlation once.
func (s *EscalationStarter) Start(ctx context.Context, r EscalationStart) (StartedEscalation, error) {
	return StartedEscalation{}, errStartNotBuilt
}
