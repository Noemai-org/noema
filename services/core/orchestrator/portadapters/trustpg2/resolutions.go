// Package trustpg2 is the Postgres side of the confirmation path (TRU-12): the store of resolved confirmations (migration 0028), read and written as the Trust login of one
// tenant under forced row security.
package trustpg2

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

// Errors of the store.
var (
	// ErrConflict: the event or its request already has a resolution that differs from this one. Nothing is written; the caller fails closed.
	ErrConflict = errors.New("trustpg2: a different resolution is already recorded")
	// ErrRequest: a field is not in the contract's form, or the context has no deadline.
	ErrRequest = errors.New("trustpg2: the resolution is not in the contract's form")
	// ErrNotBuilt is the red-commit stub's answer.
	ErrNotBuilt = errors.New("trustpg2: not built")
)

// Resolutions implements trustport.ConfirmationResolutions over a Trust login pool.
type Resolutions struct{ Pool *pgxpool.Pool }

var _ trustport.ConfirmationResolutions = Resolutions{}

// Record inserts the resolution once: the same resolution again is a no-op, a different one for the same event or request is ErrConflict.
func (r Resolutions) Record(ctx context.Context, c trustport.ResolvedConfirmation) error {
	return ErrNotBuilt
}

// Get reads one resolution as the tenant. found is false when there is none (or it is another tenant's).
func (r Resolutions) Get(ctx context.Context, tenantID, eventID string) (trustport.ResolvedConfirmation, bool, error) {
	return trustport.ResolvedConfirmation{}, false, ErrNotBuilt
}
