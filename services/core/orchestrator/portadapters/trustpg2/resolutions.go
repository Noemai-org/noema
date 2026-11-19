// Package trustpg2 is the Postgres side of the confirmation path (TRU-12): the store of resolved confirmations (migration 0028), read and written as the Trust login of one
// tenant under forced row security.
package trustpg2

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

// Errors of the store.
var (
	// ErrConflict: the event or its request already has a resolution that differs from this one. Nothing is written; the caller fails closed.
	ErrConflict = errors.New("trustpg2: a different resolution is already recorded")
	// ErrRequest: a field is not in the contract's form, or the context has no deadline.
	ErrRequest = errors.New("trustpg2: the resolution is not in the contract's form")
)

// Resolutions implements trustport.ConfirmationResolutions over a Trust login pool.
type Resolutions struct{ Pool *pgxpool.Pool }

var _ trustport.ConfirmationResolutions = Resolutions{}

var (
	tenantRe  = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`)
	eventRe   = regexp.MustCompile(`^cfe_[0-9A-Za-z]{2,32}$`)
	requestRe = regexp.MustCompile(`^cfr_[0-9A-Za-z]{2,32}$`)
	hashRe    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const uniqueViolation = "23505"

// Record inserts the resolution once: the same resolution again is a no-op, a different one for the same event or request is ErrConflict. The tenant is the login's: the
// database refuses a row of another tenant (migration 0028).
func (r Resolutions) Record(ctx context.Context, c trustport.ResolvedConfirmation) error {
	if r.Pool == nil {
		return trustport.ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("%w: the context needs a deadline", ErrRequest)
	}
	if !tenantRe.MatchString(c.TenantID) || !eventRe.MatchString(c.EventID) || !requestRe.MatchString(c.RequestID) || !hashRe.MatchString(c.BindingHash) ||
		c.AcceptedAt.IsZero() || c.ExpiresAt.IsZero() || c.ExpiresAt.Before(c.AcceptedAt) {
		return ErrRequest
	}
	tag, err := r.Pool.Exec(ctx, `INSERT INTO confirmation_resolution (tenant_id, confirmation_event_id, confirmation_request_id, binding_hash, accepted_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (tenant_id, confirmation_event_id) DO NOTHING`,
		c.TenantID, c.EventID, c.RequestID, c.BindingHash, c.AcceptedAt.UTC(), c.ExpiresAt.UTC())
	var pg *pgconn.PgError
	// A concurrent insert of the same row can surface as the request key's unique violation (the arbiter above names the event key only): either way the stored row decides.
	clash := errors.As(err, &pg) && pg.Code == uniqueViolation
	if err != nil && !clash {
		return fmt.Errorf("trustpg2: record the resolution: %w", err)
	}
	if err == nil && tag.RowsAffected() == 1 {
		return nil
	}
	got, found, err := r.Get(ctx, c.TenantID, c.EventID)
	if err != nil {
		return err
	}
	if !found || got.RequestID != c.RequestID || got.BindingHash != c.BindingHash || !got.AcceptedAt.Equal(c.AcceptedAt) || !got.ExpiresAt.Equal(c.ExpiresAt) {
		return ErrConflict
	}
	return nil
}

// Get reads one resolution as the tenant. found is false when there is none (or it is another tenant's: row security hides it).
func (r Resolutions) Get(ctx context.Context, tenantID, eventID string) (trustport.ResolvedConfirmation, bool, error) {
	if r.Pool == nil {
		return trustport.ResolvedConfirmation{}, false, trustport.ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok {
		return trustport.ResolvedConfirmation{}, false, fmt.Errorf("%w: the context needs a deadline", ErrRequest)
	}
	c := trustport.ResolvedConfirmation{TenantID: tenantID, EventID: eventID}
	err := r.Pool.QueryRow(ctx, `SELECT confirmation_request_id, binding_hash, accepted_at, expires_at FROM confirmation_resolution
		WHERE tenant_id = $1 AND confirmation_event_id = $2`, tenantID, eventID).Scan(&c.RequestID, &c.BindingHash, &c.AcceptedAt, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return trustport.ResolvedConfirmation{}, false, nil
	}
	if err != nil {
		return trustport.ResolvedConfirmation{}, false, fmt.Errorf("trustpg2: read the resolution: %w", err)
	}
	c.AcceptedAt, c.ExpiresAt = c.AcceptedAt.UTC(), c.ExpiresAt.UTC()
	return c, true, nil
}
