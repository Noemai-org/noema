// Package executorport adapts the Execution Controller to orchestrator.Executor (STM-03, A204, REQ-EXE-001). It spends and dispatches one grant by calling the
// Controller, and maps what the Controller reports to the runner's receipt class: a receipt is Applied (any receipt other than OUTCOME_UNKNOWN: the verdict is
// Outcome's, AGENT-21), a receipt of OUTCOME_UNKNOWN is Unknown (a timeout, a 5xx or silence: the runner verifies by key and never dispatches again, INV-07,
// AGENT-07), and a void is Voided (nothing was dispatched). A replay, a lost race, a spent dispatch budget, or an Outcome with nothing in it is an error: the
// runner stops and a resume reads the Record. The package decides nothing and holds no credential.
package executorport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// ErrNotUsable is returned for a port with no Controller or no timeout, a session without a tenant or principal, or a call without a grant.
var ErrNotUsable = errors.New("executorport: the port cannot be used")

// ErrNotDispatched is returned when the Controller did nothing the runner may act on: the grant was already spent (a replay), another writer wrote the attempt,
// the key's dispatch budget is spent (A153), or the Controller reported nothing.
var ErrNotDispatched = errors.New("executorport: the Controller did not dispatch the grant")

// Controller is the part of the Execution Controller the port calls.
type Controller interface {
	Execute(ctx context.Context, tenantID, grantID string) (controller.Outcome, error)
}

// Port implements orchestrator.Executor. Timeout bounds each call and comes from configuration; it has no default (AGENT-16).
type Port struct {
	Controller Controller
	Timeout    time.Duration
}

var _ orchestrator.Executor = Port{}

// Execute runs the grant through the Controller as the session's tenant.
func (p Port) Execute(ctx context.Context, s orchestrator.Session, grantID string) (orchestrator.Receipt, error) {
	if p.Controller == nil || p.Timeout <= 0 || s.TenantID == "" || s.PrincipalID == "" || grantID == "" {
		return orchestrator.Receipt{}, ErrNotUsable
	}
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	out, err := p.Controller.Execute(ctx, s.TenantID, grantID)
	if err != nil {
		return orchestrator.Receipt{}, fmt.Errorf("executorport: execute %s: %w", grantID, err)
	}
	switch {
	case out.Void != "":
		return orchestrator.Receipt{GrantID: grantID, Class: orchestrator.ReceiptVoided}, nil
	case out.Replayed:
		return orchestrator.Receipt{}, fmt.Errorf("%w: the grant was already spent", ErrNotDispatched)
	case out.Raced:
		return orchestrator.Receipt{}, fmt.Errorf("%w: another writer wrote this attempt's receipt", ErrNotDispatched)
	case out.BudgetSpent:
		return orchestrator.Receipt{}, fmt.Errorf("%w: the key's dispatch budget is spent", ErrNotDispatched)
	case out.Receipt == nil:
		return orchestrator.Receipt{}, fmt.Errorf("%w: no receipt", ErrNotDispatched)
	}
	switch out.Receipt.State {
	case controller.StateOutcomeUnknown:
		return orchestrator.Receipt{GrantID: grantID, Class: orchestrator.ReceiptUnknown}, nil
	case controller.StateDispatched, controller.StateAcknowledged, controller.StateRejectedByTarget, controller.StateRejectedStaleVersion, controller.StateFailedTerminal:
		return orchestrator.Receipt{GrantID: grantID, Class: orchestrator.ReceiptApplied}, nil
	}
	return orchestrator.Receipt{}, fmt.Errorf("%w: receipt state %q", ErrNotDispatched, out.Receipt.State)
}
