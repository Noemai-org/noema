package trustport

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoTenant is returned when the tenant's login sees no tenant row: the switch is not known, which is not "off".
var ErrNoTenant = errors.New("trustport: the tenant row is not visible")

var packRefRe = regexp.MustCompile(`^[a-z][a-z0-9]*@[0-9]+\.[0-9]+\.[0-9]+$`)

// KillSwitch answers the second question of T3, intake.Writes.Enabled: are writes enabled for the tenant, that is, is the tenant's kill switch off and is the pack loaded (PERD 23.3).
// It reads `tenant.kill_switch` as the tenant's own trust_rw login (the same read the Decision Record writer makes before it records a decision with steps, trust/recorder). It is
// half of intake.Writes: `NeedsWrite`, the question whether an intent leads to a write, has no rule in the pack today and is not built (status/TRU-adapters-intents-writes-ownership.md).
type KillSwitch struct {
	pools   TenantPools
	timeout time.Duration
}

// NewKillSwitch builds the adapter. What cmd/core must pass:
//
//   - pools: the same one trust_rw login per tenant as NewStoredIntents (EnvTrustDSNDir, A228). The adapter asks for the tenant the session names, and the login must be that tenant's.
//   - packRef: `<pack id>@<version>` of the pack the process loaded. "The pack loaded" is a condition of the adapter: it is not built without one, and a process whose pack failed to
//     load has no adapter, so no write can be enabled.
//   - timeout: the deadline of one read, from configuration (AGENT-16).
//
// A missing part is ErrConfig.
func NewKillSwitch(pools TenantPools, packRef string, timeout time.Duration) (*KillSwitch, error) {
	if pools == nil || timeout <= 0 || !packRefRe.MatchString(packRef) {
		return nil, ErrConfig
	}
	return &KillSwitch{pools: pools, timeout: timeout}, nil
}

// Enabled is true only when the tenant's row is visible and its kill switch is off. A switch that cannot be read is false with an error (never true), a tenant the login does not
// see is false with ErrNoTenant, a tenant with no login is false with ErrNoPool, and an expired or cancelled context is that error: T3 ends UNKNOWN on every one of them
// (REQ-NFR-002). The tenant is the argument, which the intake takes from the authenticated session (INV-12), and the statement is bound to it.
func (k *KillSwitch) Enabled(ctx context.Context, tenantID string) (bool, error) {
	if k == nil || k.pools == nil || k.timeout <= 0 {
		return false, ErrConfig
	}
	if tenantID == "" {
		return false, ErrSession
	}
	q, err := k.pools.For(tenantID)
	if err != nil {
		return false, fmt.Errorf("trustport: the tenant's login: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, k.timeout)
	defer cancel()
	var killed bool
	err = q.QueryRow(ctx, `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, tenantID).Scan(&killed)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, ErrNoTenant
	case err != nil:
		return false, storeErr(ctx)
	}
	return !killed, nil
}
