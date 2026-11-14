// Package journalport is the Record-backed orchestrator.Journal (STM-03, A204): it reads GRANT_SPENT and DISPATCH_ATTEMPTED for one grant from the Record, as the
// tenant's record_read login, in a read-only REPEATABLE READ transaction, and verifies the chain it read from genesis before it believes it (REQ-AUD-003). The
// Record is the truth: a chain that does not verify is an error, never an empty Progress. The package writes nothing and holds no credential of its own.
package journalport

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// ErrNotUsable is returned for a port with no database, a session without a tenant or principal, or a call without an investigation or a grant.
var ErrNotUsable = errors.New("journalport: the port cannot be used")

const (
	eventGrantSpent        = "GRANT_SPENT"
	eventDispatchAttempted = "DISPATCH_ATTEMPTED"
)

// TxBeginner is a pool of the tenant's record_read login.
type TxBeginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Port implements orchestrator.Journal.
type Port struct{ DB TxBeginner }

var _ orchestrator.Journal = Port{}

// Progress reports what the Record holds for the grant. The chain is read as the login's own tenant and must be the session's.
func (p Port) Progress(ctx context.Context, s orchestrator.Session, investigationID, grantID string) (orchestrator.Progress, error) {
	if p.DB == nil || s.TenantID == "" || s.PrincipalID == "" || investigationID == "" || grantID == "" {
		return orchestrator.Progress{}, ErrNotUsable
	}
	if _, ok := ctx.Deadline(); !ok {
		return orchestrator.Progress{}, fmt.Errorf("%w: the context needs a deadline", ErrNotUsable)
	}
	tx, err := p.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return orchestrator.Progress{}, fmt.Errorf("journalport: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := chain.ReadChain(ctx, tx)
	if err != nil {
		return orchestrator.Progress{}, fmt.Errorf("journalport: read the Record: %w", err)
	}
	if _, err := chain.VerifyRows(s.TenantID, rows); err != nil {
		return orchestrator.Progress{}, fmt.Errorf("journalport: the Record does not verify: %w", err)
	}
	var out orchestrator.Progress
	for _, r := range rows {
		if r.ObjectRef != grantID {
			continue
		}
		switch r.EventType {
		case eventGrantSpent:
			out.GrantSpent = true
		case eventDispatchAttempted:
			out.DispatchAttempted = true
		}
	}
	return out, nil
}
