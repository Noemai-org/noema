package trustpg

import (
	"context"
	"errors"
	"fmt"
	"time"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

// Intents implements trustport.Intents: it reads an admitted intent as the tenant's trust_rw login. It fills only what the adapter reads (the keys, the node, the hash and the
// correlation the intent was admitted under); it is not the intake's own Intents port, which also needs the intent's view and target.
type Intents struct {
	pools   Pools
	timeout time.Duration
}

var _ trustport.Intents = (*Intents)(nil)

// NewIntents builds the reader. No pool or a timeout below 1ns is ErrConfig.
func NewIntents(p Pools, timeout time.Duration) (*Intents, error) {
	if err := p.usable(timeout); err != nil {
		return nil, err
	}
	return &Intents{pools: p, timeout: timeout}, nil
}

// Get reads one version. An intent that is absent or another tenant's is intake.ErrNotFound; a tenant with no login is ErrNoPool.
func (i *Intents) Get(ctx context.Context, tenantID, intentID string, version int) (intake.AdmittedIntent, error) {
	if i == nil || tenantID == "" {
		return intake.AdmittedIntent{}, ErrConfig
	}
	pool, err := i.pools.For(tenantID)
	if err != nil {
		return intake.AdmittedIntent{}, err
	}
	a, err := (&intentstore.Reader{Pool: pool, Timeout: i.timeout}).Get(ctx, intentID, version)
	if errors.Is(err, intentstore.ErrNotFound) {
		return intake.AdmittedIntent{}, intake.ErrNotFound
	}
	if err != nil {
		return intake.AdmittedIntent{}, fmt.Errorf("trustpg: read the intent: %w", err)
	}
	if a.TenantID != tenantID || a.IntentID != intentID || a.Version != version {
		return intake.AdmittedIntent{}, intake.ErrNotFound
	}
	return intake.AdmittedIntent{TenantID: a.TenantID, IntentID: a.IntentID, Version: a.Version, IntentNode: a.IntentNode, IntentHash: a.IntentHash, CorrelationID: a.CorrelationID}, nil
}
