package controller

import (
	"context"
	"fmt"
	"time"
)

// Recover is the recovery sweeper of PERD section 27.1: a spent attempt with no receipt (the process died
// between E8 and E11) gets a receipt with OUTCOME_UNKNOWN, which Outcome then verifies against the target.
// It never dispatches and has no reference to the Dispatcher (EC-E09, REQ-EXE-006). It returns how many
// receipts it wrote; a receipt another writer wrote first is not an error.
func (c *Controller) Recover(ctx context.Context, tenantID string) (int, error) {
	if _, ok := ctx.Deadline(); !ok {
		return 0, fmt.Errorf("%sthe context needs a deadline", errPrefix)
	}
	pending, err := c.Ledger.Dispatching(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("%slist dispatching: %w", errPrefix, err)
	}
	written := 0
	for _, p := range pending {
		rc, err := c.sign(Receipt{ReceiptID: c.IDs.ReceiptID(), GrantID: p.GrantID, TenantID: p.TenantID, CorrelationID: p.CorrelationID, IdempotencyKey: p.IdempotencyKey,
			LogicalAction: p.LogicalAction, Attempt: p.Attempt, DispatchedAt: c.Clock.Now().UTC().Format(time.RFC3339Nano),
			TransportResult: transportSweeperNoReceipt, State: StateOutcomeUnknown})
		if err != nil {
			return written, err
		}
		ok, err := c.Ledger.Complete(ctx, rc)
		if err != nil {
			return written, fmt.Errorf("%scomplete: %w", errPrefix, err)
		}
		if ok {
			written++
		}
	}
	return written, nil
}
