package trustport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// TRU-02 red stub: the rule is not built yet.

// ErrWriteUnevaluable is not built yet.
var ErrWriteUnevaluable = errors.New("trustport: the write_when predicate cannot be evaluated")

// PackWriteRule is not built yet.
type PackWriteRule struct{}

var _ WriteRule = (*PackWriteRule)(nil)

// NewPackWriteRule is not built yet.
func NewPackWriteRule(nodes map[string]trustpack.WriteWhen) (*PackWriteRule, error) {
	return &PackWriteRule{}, nil
}

// MayWrite is not built yet.
func (r *PackWriteRule) MayWrite(context.Context, intake.AdmittedIntent) (bool, error) {
	return false, nil
}
