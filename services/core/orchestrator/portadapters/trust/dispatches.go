package trustport

import (
	"context"
	"fmt"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// KeyDispatches is how many times the ledger has spent a grant for the key (the GRANT_SPENT events that name the key's idempotency key, REQ-RCV-003). It counts and
// decides nothing.
type KeyDispatches interface {
	Used(ctx context.Context, k intake.DispatchKey) (int, error)
}

// Dispatches implements intake.Dispatches. The limit is the one number the composition holds (orchestrator.PERDMaxDispatchesPerKey) and is the same for every capability,
// a read included: the intake fails closed on a limit below 1 (A306), and a read's key is simply never used.
type Dispatches struct {
	keys  KeyDispatches
	limit int
}

var _ intake.Dispatches = Dispatches{}

// NewDispatches builds the adapter. A missing ledger or a limit below 1 is ErrConfig.
func NewDispatches(keys KeyDispatches, limit int) (Dispatches, error) {
	if keys == nil || limit < 1 {
		return Dispatches{}, ErrConfig
	}
	return Dispatches{keys: keys, limit: limit}, nil
}

// Count answers the used and the allowed dispatches of one key. A ledger failure is an error and a negative count is ErrBadAnswer, never a count.
func (d Dispatches) Count(ctx context.Context, k intake.DispatchKey) (intake.DispatchCount, error) {
	if d.keys == nil || d.limit < 1 {
		return intake.DispatchCount{}, ErrConfig
	}
	if k.TenantID == "" {
		return intake.DispatchCount{}, ErrSession
	}
	used, err := d.keys.Used(ctx, k)
	if err != nil {
		return intake.DispatchCount{}, fmt.Errorf("trustport: count the dispatches: %w", err)
	}
	if used < 0 {
		return intake.DispatchCount{}, fmt.Errorf("%w: a negative dispatch count", ErrBadAnswer)
	}
	return intake.DispatchCount{Used: used, Limit: d.limit}, nil
}
