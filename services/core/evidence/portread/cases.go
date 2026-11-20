package portread

import (
	"context"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
)

// CaseSources are the two list sources of a case read: the case list and the complaint list, as the pack's evidence descriptors give them.
type CaseSources struct{ Case, Complaint Source }

// ReadCases is not built yet (STUB of the red commit).
func (r *Reader) ReadCases(ctx context.Context, scope adapters.Scope, correlationID, transactionRef string) ([]layer.Item, error) {
	return nil, nil
}
