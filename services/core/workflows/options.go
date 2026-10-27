package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// writeOptions are the options of a write activity: one attempt, never a Temporal retry (D8, REQ-EXE-006). The
// literal 1 is what the retry scanner (retry_test.go) holds every write activity to.
func writeOptions(ctx workflow.Context, timeout time.Duration) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: timeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
}

// readOptions are the options of a read activity: bounded attempts with the backoff of the Activities table. The
// scanner needs MaximumAttempts as a literal, so the literal below is held equal to the table by a test.
func readOptions(ctx workflow.Context, name string, timeout time.Duration) workflow.Context {
	policy, ok := PolicyFor(name)
	if !ok || policy.Kind != Read || len(policy.Policy.BackoffSeconds) == 0 {
		// An undeclared or non-read activity gets the strictest option, never a retry.
		return writeOptions(ctx, timeout)
	}
	first := policy.Policy.BackoffSeconds[0]
	coefficient := 1.0
	if len(policy.Policy.BackoffSeconds) > 1 && first > 0 {
		coefficient = float64(policy.Policy.BackoffSeconds[1]) / float64(first)
	}
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: timeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    time.Duration(first) * time.Second,
			BackoffCoefficient: coefficient,
		},
	})
}
