package controller_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// noema#714, Architect ruling A339 (relayed; interim), REQ-EXE-001, REQ-EXE-002, INV-03, INV-07: a Ledger that could not commit a spend because the database kept refusing it
// with a serialization failure returns controller.ErrSpendRetryable, wrapped around the database's error. Execute passes it on so that its caller can tell "present the grant
// again" from a refusal. It is not a void, a receipt or an outcome: the grant stays unspent, nothing is dispatched. Everything is SYNTHETIC.

type retryableLedger struct{ *World }

var errDatabase = errors.New("ERROR: could not serialize access (SQLSTATE 40001)")

func (retryableLedger) Spend(context.Context, controller.SpendCommand) (controller.SpendResult, error) {
	return controller.SpendResult{}, fmt.Errorf("ledger: spend did not commit: %w: %w", controller.ErrSpendRetryable, errDatabase)
}

func TestExecutePassesTheRetryableSpendErrorOnAndDispatchesNothing(t *testing.T) {
	w, c, g := setup(t)
	c.Ledger = retryableLedger{w}
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if !errors.Is(err, controller.ErrSpendRetryable) || !errors.Is(err, errDatabase) {
		t.Fatalf("Execute error = %v; want one that is controller.ErrSpendRetryable and carries the database's error", err)
	}
	if out.Void != "" || out.Receipt != nil || out.Replayed || out.Raced || out.BudgetSpent {
		t.Fatalf("outcome %+v; a retryable spend error is no void, receipt or other outcome", out)
	}
	s := w.Snapshot()
	if len(s.Dispatches) != 0 || len(s.Effects) != 0 || len(s.Recovery) != 0 || len(s.Voids) != 0 || s.Spent[g.Grant.GrantID] {
		t.Fatalf("a spend that did not commit changed the world: %+v", s)
	}
}

// Another spend error is not the retryable one: the typed error is never inferred.
func TestExecuteDoesNotCallAnyOtherSpendErrorRetryable(t *testing.T) {
	w, c, g := setup(t)
	c.Ledger = failingLedger{w}
	_, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err == nil || errors.Is(err, controller.ErrSpendRetryable) {
		t.Fatalf("Execute error = %v; want an error that is not retryable", err)
	}
}

type failingLedger struct{ *World }

func (failingLedger) Spend(context.Context, controller.SpendCommand) (controller.SpendResult, error) {
	return controller.SpendResult{}, errDatabase
}

// The code an edge reports for the retryable error is one the error contract lists as retryable: no code was added for it, and the code cannot drift out of the contract.
func TestTheRetryableSpendCodeIsARetryableCodeOfTheErrorContract(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/schemas/errors.v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(doc.Defs["RetryableErrorCode"].Enum, controller.CodeSpendRetryable) || !slices.Contains(doc.Defs["ErrorCode"].Enum, controller.CodeSpendRetryable) {
		t.Fatalf("%q is not a RetryableErrorCode of errors.v2.json", controller.CodeSpendRetryable)
	}
}
