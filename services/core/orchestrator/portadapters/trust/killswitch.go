package trustport

import (
	"context"
	"errors"
	"time"
)

// ErrNoTenant is returned when the tenant's login sees no tenant row.
var ErrNoTenant = errors.New("trustport: the tenant row is not visible")

// KillSwitch answers intake.Writes.Enabled.
type KillSwitch struct{}

// NewKillSwitch builds the adapter.
func NewKillSwitch(pools TenantPools, packRef string, timeout time.Duration) (*KillSwitch, error) {
	return &KillSwitch{}, nil
}

// Enabled is intake.Writes.Enabled.
func (k *KillSwitch) Enabled(context.Context, string) (bool, error) { return false, errStub }
