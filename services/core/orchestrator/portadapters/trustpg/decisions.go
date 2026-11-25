package trustpg

import (
	"context"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

// Decisions reads the ALLOW decision of a correlation, for recovery (A696). It is the red stub: it answers nothing.
type Decisions struct {
	pools   Pools
	timeout time.Duration
}

// NewDecisions builds the reader. No pool or a timeout below 1ns is ErrConfig.
func NewDecisions(p Pools, timeout time.Duration) (*Decisions, error) {
	if err := p.usable(timeout); err != nil {
		return nil, err
	}
	return &Decisions{pools: p, timeout: timeout}, nil
}

// AllowOf is the red stub.
func (d *Decisions) AllowOf(_ context.Context, _, _ string) (orchestrator.Decision, error) {
	return orchestrator.Decision{}, trustport.ErrNoRecord
}
