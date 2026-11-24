package trustpg

import (
	"context"
	"errors"
	"time"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

// Dispositions implements trustport.Dispositions over the Decision Record (A539; REQ-TRU-010, INV-12, INV-30): the ESCALATE disposition decision of a correlation, read as the tenant's
// own trust_rw login under forced row-level security. Read-only.
type Dispositions struct {
	pools   Pools
	timeout time.Duration
}

var _ trustport.Dispositions = (*Dispositions)(nil)

// NewDispositions builds the reader. No pool or a timeout below 1ns is ErrConfig.
func NewDispositions(p Pools, timeout time.Duration) (*Dispositions, error) {
	if err := p.usable(timeout); err != nil {
		return nil, err
	}
	return &Dispositions{pools: p, timeout: timeout}, nil
}

// DispositionFor is not built yet.
func (d *Dispositions) DispositionFor(context.Context, string, string) (string, error) {
	return "", errors.New("trustpg: not built")
}
