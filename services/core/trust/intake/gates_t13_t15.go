package intake

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// lateState is what T3 and T13 to T15 learn as the call goes. It is private to one call.
type lateState struct {
	needsWrite     bool         // T3: the request leads to a write
	plan           *Plan        // T14: the validated plan
	confirmationID string       // T13: the event of the confirmation the ALLOW follows
	recorded       RecordResult // T15: what the Recorder wrote
}

func (e *evaluation) t13(context.Context) decide.StepResult { return decide.FailStep(ErrNotBuilt) }
func (e *evaluation) t14(context.Context) decide.StepResult { return decide.FailStep(ErrNotBuilt) }
func (e *evaluation) t15(context.Context) decide.StepResult { return decide.FailStep(ErrNotBuilt) }
