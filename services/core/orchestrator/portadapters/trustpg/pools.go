package trustpg

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrConfig is returned for a reader built without what it needs: no pool, no Record reader or no positive timeout. A timeout comes from configuration and has no default (AGENT-16).
	ErrConfig = errors.New("trustpg: the reader is not configured")
	// ErrNoPool is returned for a tenant that has no login. It never falls back to another tenant's login (INV-12), and it is not "no record": the caller could not tell.
	ErrNoPool = errors.New("trustpg: no login is configured for this tenant")
	// ErrBadKey is returned for a dispatch key with no tenant, correlation, capability or target: it might name any key, so it is never counted as zero.
	ErrBadKey = errors.New("trustpg: the dispatch key is incomplete")
)

// Pools holds one trust_rw pool per tenant, each logging in as that tenant's own login.
type Pools struct{ byTenant map[string]*pgxpool.Pool }

// NewPools copies the map. An empty map, or a tenant without a pool, is ErrConfig.
func NewPools(m map[string]*pgxpool.Pool) (Pools, error) {
	if len(m) == 0 {
		return Pools{}, fmt.Errorf("%w: no tenant login", ErrConfig)
	}
	c := make(map[string]*pgxpool.Pool, len(m))
	for t, p := range m {
		if t == "" || p == nil {
			return Pools{}, fmt.Errorf("%w: a tenant without a pool", ErrConfig)
		}
		c[t] = p
	}
	return Pools{byTenant: c}, nil
}

// For returns the tenant's own pool, or ErrNoPool.
func (p Pools) For(tenantID string) (*pgxpool.Pool, error) {
	pool, ok := p.byTenant[tenantID]
	if !ok {
		return nil, ErrNoPool
	}
	return pool, nil
}

func (p Pools) usable(timeout time.Duration) error {
	if len(p.byTenant) == 0 || timeout <= 0 {
		return ErrConfig
	}
	return nil
}
