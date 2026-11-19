package policystore

import (
	"context"
	"errors"
	"fmt"

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

// Published is every version the tenant has published for the policy, oldest first, ordered by the numeric key the database compares (1.9 before 1.10). PublishedAt is the
// database's time in whole Unix seconds, dropped and never rounded up: a version is not in force before it was recorded. A policy with no publication gives an empty list; a read
// that fails is an error and never an empty list. The tenant is also bound by the login, so a read of another tenant's policy through one's own login finds nothing.
func Published(ctx context.Context, q Querier, tenantID, policyID string) ([]Publication, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New(errPrefix + "the context needs a deadline")
	}
	if !tenantRe.MatchString(tenantID) || !policyIDRe.MatchString(policyID) {
		return nil, fmt.Errorf("%w: a tenant or policy ID is outside its pattern", ErrRequest)
	}
	rows, err := q.Query(ctx, `SELECT version, floor(extract(epoch FROM published_at))::bigint FROM policy_publication
		WHERE tenant_id = $1 AND policy_id = $2 ORDER BY version_key ASC`, tenantID, policyID)
	if err != nil {
		return nil, &storeError{text: errPrefix + "the publications could not be read", cause: err}
	}
	defer rows.Close()
	var out []Publication
	for rows.Next() {
		var p Publication
		if err := rows.Scan(&p.Version, &p.PublishedAt); err != nil {
			return nil, &storeError{text: errPrefix + "a publication could not be read", cause: err}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, &storeError{text: errPrefix + "the publications could not be read", cause: err}
	}
	return out, nil
}
