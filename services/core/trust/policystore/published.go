package policystore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Publication is one version of a policy a tenant has published: the version as written and the time the database recorded it, in Unix seconds.
type Publication struct {
	Version     string
	PublishedAt int64
}

// Querier is the read side that returns several rows: a pool, a connection or a transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Published is every version of the tenant's policy, oldest first.
func Published(ctx context.Context, q Querier, tenantID, policyID string) ([]Publication, error) {
	return nil, errors.New(errPrefix + "Published is not built")
}
