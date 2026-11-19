package trustport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// ErrNoWriteAnswer is what a WriteRule returns for an intent node it has no answer for: an unknown node, an unloaded pack, or a node the pack does not classify.
var ErrNoWriteAnswer = errors.New("trustport: the pack gives no write answer for this intent node")

// WriteRule answers, per intent node, whether the node may lead to a write.
type WriteRule interface {
	MayWrite(intentNode string) (bool, error)
}

// WritesSwitch answers whether writes are enabled for a tenant. *KillSwitch is one.
type WritesSwitch interface {
	Enabled(ctx context.Context, tenantID string) (bool, error)
}

// Writes implements intake.Writes.
type Writes struct {
	sw   WritesSwitch
	rule WriteRule
}

var _ intake.Writes = (*Writes)(nil)

// NewWrites builds the adapter.
func NewWrites(sw WritesSwitch, rule WriteRule) (*Writes, error) {
	return &Writes{sw: sw, rule: rule}, nil
}

// NeedsWrite is the write rule's answer for the intent's node.
func (w *Writes) NeedsWrite(ctx context.Context, a intake.AdmittedIntent) (bool, error) {
	return false, nil
}

// Enabled is the kill switch's answer.
func (w *Writes) Enabled(ctx context.Context, tenantID string) (bool, error) {
	return false, nil
}
