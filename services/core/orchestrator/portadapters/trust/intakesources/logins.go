package intakesources

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Querier reads one row as one tenant's own Trust login; *pgxpool.Pool is the real one.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Logins gives the Querier of a tenant's own Trust login (trust_rw). A tenant with no login is an error: there is no fallback to another tenant's login.
type Logins interface {
	For(tenantID string) (Querier, error)
}

// reader is the one place that reads: the checks every read shares, the tenant's login and the bound of the call.
type reader struct {
	logins  Logins
	timeout time.Duration
}

func newReader(logins Logins, timeout time.Duration) (reader, error) {
	return reader{logins: logins, timeout: timeout}, nil
}
