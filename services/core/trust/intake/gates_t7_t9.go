package intake

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// t7, t8 and t9 are not built yet in this commit: they fail closed.
func (e *evaluation) t7(context.Context) decide.StepResult { return decide.FailStep(ErrNotBuilt) }
func (e *evaluation) t8(context.Context) decide.StepResult { return decide.FailStep(ErrNotBuilt) }
func (e *evaluation) t9(context.Context) decide.StepResult { return decide.FailStep(ErrNotBuilt) }
