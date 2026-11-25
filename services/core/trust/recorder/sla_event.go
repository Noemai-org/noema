package recorder

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/trust/clocks"
)

// SLARequest is one clock evaluation to record (STUB for the red commit).
type SLARequest struct {
	TenantID      string
	CorrelationID string
	Evaluation    clocks.Evaluation
}

// AppendSLAEvaluated is not built yet.
func (w *Writer) AppendSLAEvaluated(context.Context, pgx.Tx, SLARequest) error {
	return errors.New("not built")
}
