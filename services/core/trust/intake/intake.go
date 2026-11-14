package intake

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// Service evaluates requests. Its zero value cannot run.
type Service struct {
	Ports       Ports
	Clock       clock.Clock
	PackVersion string
}

// Result is the outcome of one evaluation: the decision and the trace of the steps it reached.
type Result struct {
	Decision decide.Decision
	Trace    []decide.TraceEntry
}

// ErrNotBuilt is what a step that does not exist yet returns: the evaluation ends in UNKNOWN INTERNAL_ERROR (fail closed).
var ErrNotBuilt = errors.New("intake: this step is not built")

// ErrNoDeadline is returned when the context has no deadline.
var ErrNoDeadline = errors.New("intake: the context needs a deadline")

// Evaluate is not built yet (red first: noema#TRU-02 intake).
func (s *Service) Evaluate(ctx context.Context, r Request) (Result, error) {
	return Result{}, ErrNotBuilt
}
