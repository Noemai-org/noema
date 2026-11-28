// Package resumestate answers whether an investigation has ended, for the follow-up resolver (understanding/resume.States; INP-01 slice 6; REQ-INP-004; A320, A404; INV-12). This is the stub of the
// red commit: it names the types and answers nothing yet.
package resumestate

import (
	"context"
	"errors"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
)

// ErrRead is a state that could not be read or decided. The cause stays reachable.
var ErrRead = errors.New("resumestate: the state of the investigation could not be read")

// Reader is the investigation store's read (investigations.Store.Read implements it).
type Reader interface {
	Read(ctx context.Context, sess orchestrator.Session, investigationID string) (investigations.Snapshot, error)
}

// Table is the orchestrator's transition table (orchestrator.Table implements it).
type Table interface {
	States() []orchestrator.State
	Exits(from orchestrator.State) []orchestrator.State
}

// States implements understanding/resume.States over the investigation row.
type States struct {
	reader  Reader
	table   Table
	timeout time.Duration
}

// New builds a States; every part is required.
func New(r Reader, t Table, timeout time.Duration) (*States, error) { return &States{}, nil }

// Terminal is not built yet.
func (s *States) Terminal(ctx context.Context, tenantID, principalID, correlationID string) (bool, error) {
	return false, nil
}
