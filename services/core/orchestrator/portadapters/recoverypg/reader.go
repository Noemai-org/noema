// Package recoverypg is the Postgres reader of the recovery pass (A696; STM-05; REQ-STM-001, REQ-RCV-003, REQ-SEC-004; INV-03, INV-12). This is the red stub: it answers nothing.
package recoverypg

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
)

// ErrConfig and ErrNoPool are the red stubs' errors.
var (
	ErrConfig = errors.New("recoverypg: the reader is not configured")
	ErrNoPool = errors.New("recoverypg: no login is configured for this tenant")
)

// Decisions is the Trust-side read: the ALLOW decision of a correlation, or trustport.ErrNoRecord.
type Decisions interface {
	AllowOf(ctx context.Context, tenantID, correlationID string) (orchestrator.Decision, error)
}

// Reader implements recovery.Reader.
type Reader struct{}

var _ recovery.Reader = (*Reader)(nil)

// New is the red stub.
func New(_ map[string]*pgxpool.Pool, _ Decisions, _ []orchestrator.State, _ time.Duration) (*Reader, error) {
	return &Reader{}, nil
}

// Idle is the red stub.
func (r *Reader) Idle(_ context.Context, _ string, _ int64, _ int) ([]recovery.Candidate, error) {
	return nil, nil
}
