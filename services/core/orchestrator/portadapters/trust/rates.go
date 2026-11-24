package trustport

import (
	"context"
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
	return nil, ErrConfig
}

// Exceeded is true when the principal's grants for the capability in the last hour have reached the descriptor's limit.
func (r *Rates) Exceeded(ctx context.Context, q intake.RateQuery) (bool, error) {
	return false, ErrConfig
}
