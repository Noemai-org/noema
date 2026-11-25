// Package epochport adapts the Evidence reader's case list to the Interpreter's Epochs port (UND-04; rulings A520, A513, A482, interim, relayed, not yet committed; REQ-INT-001,
// REQ-SEC-004; INV-12, INV-19). It lives under evidence because the case-list types are the adapters', which only execution and evidence may import.
package epochport

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// ErrRefused marks a subject the adapter will not turn into a transaction reference, and a half setup. The text is fixed and names no value.
var ErrRefused = errors.New("epochport: this subject or setup is refused")

// Cases is what the adapter needs of evidence/portread.Reader.
type Cases interface {
	NextEpoch(ctx context.Context, scope adapters.Scope, transactionRef string) (int, error)
}

// Setup is the case-list read, the resource kind of a subject and the bound of one read. Every field is required.
type Setup struct {
	Cases   Cases
	Kind    string
	Timeout time.Duration
}

// Epochs implements interpreter.Epochs over the case list.
type Epochs struct{ s Setup }

// New refuses a setup with a missing part: there are no defaults (AGENT-16).
func New(s Setup) (*Epochs, error) {
	if s.Cases == nil || s.Kind == "" || s.Timeout <= 0 {
		return nil, ErrRefused
	}
	return &Epochs{s: s}, nil
}

// EpochOf answers the epoch of a new intent about the subject `<vertical>.<kind>:<id>`. The case list is keyed by the id; the kind must be the setup's. The scope is the tenant and
// principal the caller passes, which the Interpreter takes from the verified session (INV-12); an incomplete one reads nothing. The read is bounded by the setup's timeout. A list that
// cannot be relied on is portread.ErrEpochUnknown, never 1 (INV-19).
func (e *Epochs) EpochOf(ctx context.Context, tenantID, principalID, subjectRef string) (int, error) {
	kind, id, ok := strings.Cut(subjectRef, ":")
	if !ok || kind != e.s.Kind || id == "" || strings.Contains(id, ":") || tenantID == "" || principalID == "" {
		return 0, ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, e.s.Timeout)
	defer cancel()
	return e.s.Cases.NextEpoch(ctx, adapters.Scope{TenantID: tenantID, PrincipalID: principalID}, id)
}
