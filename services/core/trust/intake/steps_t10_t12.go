package intake

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func (e *evaluation) t10(context.Context) decide.StepResult { return decide.FailStep(ErrNotBuilt) }
func (e *evaluation) t11(context.Context) decide.StepResult { return decide.FailStep(ErrNotBuilt) }
func (e *evaluation) t12(context.Context) decide.StepResult { return decide.FailStep(ErrNotBuilt) }
