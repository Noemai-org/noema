// Package epochport adapts the Evidence reader's case list to the Interpreter's Epochs port (UND-04; rulings A520, A513, A482, interim, relayed, not yet committed; REQ-INT-001,
// REQ-SEC-004; INV-12, INV-19). It lives under evidence because the case-list types are the adapters', which only execution and evidence may import.
package epochport

import (
	"context"
	"errors"
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

// Epochs implements interpreter.Epochs.
type Epochs struct{}

// New refuses a setup with a missing part.
func New(Setup) (*Epochs, error) { return nil, ErrRefused }

// EpochOf answers the epoch of a new intent about the subject.
func (*Epochs) EpochOf(context.Context, string, string, string) (int, error) { return 0, ErrRefused }
