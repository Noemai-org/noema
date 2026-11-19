package intakesources

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// Querier reads one row as one tenant's own Trust login; *pgxpool.Pool is the real one.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Logins gives the Querier of a tenant's own Trust login (trust_rw). A tenant with no login is an error: there is no fallback to another tenant's login. The database, not this
// package, binds each read to the tenant (migration 0026: row-level security through the login's own mapping); the queries also name the tenant.
type Logins interface {
	For(tenantID string) (Querier, error)
}

// reader is the one place that reads: the tenant's login, the deadline of the caller and the bound of the call.
type reader struct {
	logins  Logins
	timeout time.Duration
}

// newReader refuses a missing login source and a timeout below one: the bound comes from configuration (NOEMA_TRUST_DB_TIMEOUT_MS) and has no default (AGENT-16).
func newReader(logins Logins, timeout time.Duration) (reader, error) {
	if logins == nil || timeout <= 0 {
		return reader{}, ErrConfig
	}
	return reader{logins: logins, timeout: timeout}, nil
}

// checkKey refuses a tenant, an intent or a version outside the shapes the stores hold, before any login is asked. The intent form is the one parser of an intent reference
// (contracts.ParseIntentRef, ruling A319): an ID with an `@` in it, a version below one or above the form's own bound is not an intent version.
func checkKey(tenantID, intentID string, version int) error {
	if !chain.ValidTenantID(tenantID) {
		return fmt.Errorf("%w: the tenant is outside its shape", ErrQuery)
	}
	if _, _, err := contracts.ParseIntentRef(fmt.Sprintf("%s@%d", intentID, version)); err != nil {
		return fmt.Errorf("%w: the intent or its version is outside its shape", ErrQuery)
	}
	return nil
}

// scan reads one row as the tenant. The caller's context needs a deadline and the call is also bounded by the configured timeout. A row that does not exist, or that another tenant
// owns, is ErrNotFound; anything else that goes wrong is ErrStore with a fixed text and the cause behind errors.Is (AGENT-13).
func (r reader) scan(ctx context.Context, tenantID, query string, args []any, dest ...any) error {
	if r.logins == nil || r.timeout <= 0 {
		return ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrNoDeadline
	}
	q, err := r.logins.For(tenantID)
	if err != nil {
		return wrapped(ErrStore, err)
	}
	if q == nil {
		return wrapped(ErrStore, nil)
	}
	c, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	switch err := q.QueryRow(c, query, args...).Scan(dest...); {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return wrapped(ErrStore, err)
	}
	return nil
}
