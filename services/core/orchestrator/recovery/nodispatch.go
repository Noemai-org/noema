package recovery

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// ErrNoDispatch is what NoDispatch answers.
var ErrNoDispatch = errors.New("recovery: recovery never dispatches")

// NoDispatch is the Executor the recovery runner is wired with. Recovery verifies what the Record shows as spent; if a re-evaluation (a verdict of not achieved) issues a
// new grant, spending it is a person's decision, not a restart's (INV-03, INV-07, AGENT-08).
type NoDispatch struct{}

// Execute always refuses.
func (NoDispatch) Execute(context.Context, orchestrator.Session, string) (orchestrator.Receipt, error) {
	return orchestrator.Receipt{}, ErrNoDispatch
}
