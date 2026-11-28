// Package resumestate answers whether an investigation has ended, for the follow-up resolver (understanding/resume.States; INP-01 slice 6; REQ-INP-004; A320, A404; INV-12). An investigation has ended
// when the state its row holds has no exit in the orchestrator's transition table: the table, not a list here, decides (INV-30: the row is a projection of the Record, the table is the machine's).
package resumestate

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
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

// chainRe is the investigation's chain correlation id (A234): `corr_` and the ULID the investigation id `inv_` also carries.
var chainRe = regexp.MustCompile(`\Acorr_([0-9A-Za-z]{2,32})\z`)

// New builds a States; every part is required and there is no default (AGENT-16).
func New(r Reader, t Table, timeout time.Duration) (*States, error) {
	if r == nil || t == nil || timeout <= 0 {
		return nil, errors.New("resumestate: a reader, a transition table and a positive timeout are all required")
	}
	return &States{reader: r, table: t, timeout: timeout}, nil
}

// Terminal reads the investigation of the chain as the session's tenant and principal and says whether its state has an exit. A chain that is not `corr_<ULID>`, a session with no tenant or
// principal, a row that is missing, a state the table does not know, a row whose principal is not the session's (an old row holds none and is read by tenant alone) and a store that cannot answer
// are each ErrRead and never "open": doubt does not keep an investigation resumable. The error names no tenant, principal or state (AGENT-13); the cause stays reachable.
func (s *States) Terminal(ctx context.Context, tenantID, principalID, correlationID string) (bool, error) {
	m := chainRe.FindStringSubmatch(correlationID)
	if m == nil || tenantID == "" || principalID == "" {
		return false, ErrRead
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	snap, err := s.reader.Read(ctx, orchestrator.Session{TenantID: tenantID, PrincipalID: principalID}, "inv_"+m[1])
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrRead, err)
	}
	if snap.Principal != "" && snap.Principal != principalID {
		return false, ErrRead
	}
	if !slices.Contains(s.table.States(), snap.State) {
		return false, ErrRead
	}
	return len(s.table.Exits(snap.State)) == 0, nil
}
