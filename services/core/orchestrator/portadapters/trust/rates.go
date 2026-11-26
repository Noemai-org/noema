package trustport

import (
	"context"
	"fmt"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// RateLimits is the pack's side of the rate check: the limit a capability's descriptor declares (limits.rate_per_principal_per_hour, REQ-TRU-012). declared is false for a capability
// with no limit. *trustpack.Pack implements it.
type RateLimits interface {
	RatePerPrincipalPerHour(capabilityID string) (limit int, declared bool)
}

// Rates implements intake.Rates (TRU-11, REQ-TRU-012): has the principal used up the hourly rate of the capability. It counts the grants Trust issued to the principal for the
// capability in the last hour, on the database clock, as the tenant's own trust_rw login; the limit is the descriptor's.
type Rates struct {
	pools   TenantPools
	limits  RateLimits
	timeout time.Duration
}

var _ intake.Rates = (*Rates)(nil)

// NewRates builds the adapter. A missing part or a timeout below one is ErrConfig.
func NewRates(pools TenantPools, limits RateLimits, timeout time.Duration) (*Rates, error) {
	if pools == nil || limits == nil || timeout <= 0 {
		return nil, ErrConfig
	}
	return &Rates{pools: pools, limits: limits, timeout: timeout}, nil
}

// Exceeded is true when the principal's grants for the capability in the last hour have reached the descriptor's limit.
//
// A capability that declares no limit is not limited and nothing is read for it. The count is of every grant whatever its status, so a voided grant does not give the hour back. A
// missing tenant, principal or capability is ErrSession, a tenant with no login is ErrNoPool, and a read that fails is an error: T11 ends UNKNOWN on each of them and never reads
// "not over the rate" from an error. The statement is bound to the tenant of the query, and the tenant's own login sees only that tenant's rows (row security, migration 0012).
func (r *Rates) Exceeded(ctx context.Context, q intake.RateQuery) (bool, error) {
	if r == nil || r.pools == nil || r.limits == nil || r.timeout <= 0 {
		return false, ErrConfig
	}
	if q.TenantID == "" || q.PrincipalID == "" || q.CapabilityID == "" {
		return false, ErrSession
	}
	limit, declared := r.limits.RatePerPrincipalPerHour(q.CapabilityID)
	if !declared {
		return false, nil
	}
	pool, err := r.pools.For(q.TenantID)
	if err != nil {
		return false, fmt.Errorf("trustport: the tenant's login: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var n int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM decision_grant
		WHERE tenant_id = $1 AND json ->> 'principal_id' = $2 AND json ->> 'capability_id' = $3 AND created_at > now() - interval '1 hour'`,
		q.TenantID, q.PrincipalID, q.CapabilityID).Scan(&n)
	if err != nil {
		return false, storeErr(ctx)
	}
	return n >= limit, nil
}
