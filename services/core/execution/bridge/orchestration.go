package bridge

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/workflows"
)

// GrantExecutor runs the controller for the workflow's ExecuteGrant activity and reports its outcome as the
// workflow's plain record. It interprets nothing: a void grant, a replay, a lost race and a receipt are reported as
// they are, and the receipt state is the controller's (Execution never says succeeded).
type GrantExecutor struct {
	Controller *controller.Controller
}

var _ workflows.GrantExecutor = GrantExecutor{}

// ExecuteGrant implements workflows.GrantExecutor.
func (e GrantExecutor) ExecuteGrant(ctx context.Context, tenantID, grantID string) (workflows.GrantRun, error) {
	out, err := e.Controller.Execute(ctx, tenantID, grantID)
	if err != nil {
		return workflows.GrantRun{}, err
	}
	run := workflows.GrantRun{GrantID: grantID, Void: string(out.Void), Replayed: out.Replayed, Raced: out.Raced}
	if r := out.Receipt; r != nil {
		run.ReceiptID, run.State, run.TransportClass, run.RawCode = r.ReceiptID, string(r.State), r.TransportResult, r.RawCode
		run.IdempotencyKey, run.Attempt = r.IdempotencyKey, r.Attempt
		if r.ExternalAck != nil {
			run.ExternalAck = *r.ExternalAck
		}
	}
	return run, nil
}

// CaseReadBack reads the case an idempotency key created, through the case adapter (read-only).
type CaseReadBack struct {
	Case adapters.Case
}

var _ workflows.CaseReader = CaseReadBack{}

// ReadCaseByKey implements workflows.CaseReader. The answer must be complete and name exactly one case in the
// tenant: an incomplete page does not prove anything, and two cases for one key is a fault, never a choice.
func (r CaseReadBack) ReadCaseByKey(ctx context.Context, tenantID, idempotencyKey string) (workflows.CaseView, error) {
	page, err := r.Case.FindCases(ctx, adapters.CaseQuery{TenantID: tenantID, IdempotencyKey: idempotencyKey})
	if err != nil {
		return workflows.CaseView{}, err
	}
	if !page.Complete {
		return workflows.CaseView{}, errors.New("bridge: the case list is incomplete, so no conclusion can be drawn")
	}
	if len(page.Items) != 1 {
		return workflows.CaseView{}, fmt.Errorf("bridge: %d cases carry the key, want exactly 1", len(page.Items))
	}
	c := page.Items[0]
	if c.CaseID == "" || c.VersionToken == "" || c.IdempotencyKey != idempotencyKey || c.TenantID != tenantID {
		return workflows.CaseView{}, errors.New("bridge: the case read back does not belong to this key and tenant")
	}
	return workflows.CaseView{CaseID: c.CaseID, Reference: c.Reference, Status: c.Status, OwnerQueue: c.OwnerQueue, VersionToken: c.VersionToken}, nil
}
