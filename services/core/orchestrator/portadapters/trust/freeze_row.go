package trustport

// RES-02 (REQ-RES-002, INV-20; A642, A677, interim, relayed): the Record side of the freeze row. Red stub: it records and reads nothing.

import (
	"context"

	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

// FreezeRowConfig builds a FreezeRow.
type FreezeRowConfig struct {
	Begin    TxBeginner
	Reads    func(tenantID string) chain.Queryer
	Appender *chain.Appender
}

// FreezeRow implements reconcile.FreezeRecorder and reconcile.FreezeReader over the Record.
type FreezeRow struct{ c FreezeRowConfig }

var (
	_ reconcile.FreezeRecorder = (*FreezeRow)(nil)
	_ reconcile.FreezeReader   = (*FreezeRow)(nil)
)

// NewFreezeRow returns a FreezeRow.
func NewFreezeRow(c FreezeRowConfig) *FreezeRow { return &FreezeRow{c: c} }

// RecordedFreeze is a stub.
func (*FreezeRow) RecordedFreeze(context.Context, string, string) (reconcile.RecordedFreeze, error) {
	return reconcile.FreezeUnknown, nil
}

// RecordFreezeSet is a stub.
func (*FreezeRow) RecordFreezeSet(context.Context, reconcile.FreezeSet) error { return nil }

// RecordFreezeCleared is a stub.
func (*FreezeRow) RecordFreezeCleared(context.Context, reconcile.FreezeLift) error { return nil }
