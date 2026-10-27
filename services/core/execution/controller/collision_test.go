package controller_test

import (
	"context"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// collidingLedger refuses the spend as the Postgres Ledger does when the same key exists with other parameters.
type collidingLedger struct{ *World }

func (collidingLedger) Spend(context.Context, controller.SpendCommand) (controller.SpendResult, error) {
	return controller.SpendResult{}, controller.ErrKeyCollision
}

// collidingTarget reports the target's refusal of the same key with another payload.
type collidingTarget struct{ *World }

func (collidingTarget) Dispatch(context.Context, controller.DispatchRequest) controller.TransportResult {
	return controller.TransportResult{Class: controller.TransportRejected4xx, RawCode: "409:IDEMPOTENCY_CONFLICT", KeyCollision: true}
}

// REQ-EXE-002 security, EC-E06: the same key with other parameters is never dispatched; the grant is void.
func TestKeyCollisionAtSpendVoidsTheGrantWithoutDispatch(t *testing.T) {
	w, c, g := setup(t)
	c.Ledger = collidingLedger{w}
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Void != controller.VoidBindingMismatch {
		t.Fatalf("outcome %+v err %v, want the grant void with BINDING_MISMATCH", out, err)
	}
	if s := w.Snapshot(); len(s.Dispatches) != 0 || len(s.Effects) != 0 {
		t.Fatalf("a colliding key was dispatched: %+v", s)
	}
	if len(w.Recovery) != 1 {
		t.Fatalf("a void grant raises one RecoveryRequest: %v", w.Recovery)
	}
}

// EC-E06: when the target refuses a key collision, the Record gets a SECURITY_EVENT IDEMPOTENCY_COLLISION before the
// receipt, and the receipt is REJECTED_BY_TARGET.
func TestKeyCollisionAtTheTargetRaisesASecurityEventThenARejectedReceipt(t *testing.T) {
	w, c, g := setup(t)
	c.Out = collidingTarget{w}
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Receipt == nil || out.Receipt.State != controller.StateRejectedByTarget {
		t.Fatalf("outcome %+v err %v, want a REJECTED_BY_TARGET receipt", out, err)
	}
	var types []string
	for _, e := range w.Snapshot().Record {
		types = append(types, e.Type)
	}
	want := []string{"REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "SECURITY_EVENT", "RECEIPT"}
	if len(types) != len(want) {
		t.Fatalf("record events %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("record events %v, want %v", types, want)
		}
	}
}
