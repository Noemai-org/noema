package workflows_test

import (
	"go.temporal.io/sdk/workflow"

	"github.com/Noemai-org/noema/services/core/workflows"
)

func workflowOptions() workflow.RegisterOptions {
	return workflow.RegisterOptions{Name: workflows.EscalateWorkflowName}
}
