// Package recovery is the RecoverInvestigations activity (STM-05; REQ-STM-001, REQ-RCV-003; INV-03, INV-07, INV-12, INV-30; A204, A377 item 15, interim, relayed).
//
// May import: the standard library and orchestrator. Must never: touch Temporal or the database (the Reader port is the only way to rows), dispatch a grant, take a value
// from a request, or decide an outcome (the Verifier does).
package recovery

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// Outcome is what one pass did with one investigation.
type Outcome string

const (
	Resumed     Outcome = "RESUMED"
	NeedsPerson Outcome = "NEEDS_PERSON"
	Skipped     Outcome = "SKIPPED"
	Failed      Outcome = "FAILED"
	Refused     Outcome = "REFUSED"
)

// Item is the result for one investigation; Report is one pass for one tenant.
type Item struct {
	InvestigationID string
	Outcome         Outcome
}
type Report struct{ Items []Item }

// Candidate is what the Reader returns for one idle, non-terminal investigation: the stored row and the decision Trust recorded for it.
type Candidate struct {
	Investigation                                           orchestrator.Investigation
	TenantID, CorrelationID, InputReceivedRef, PrincipalRef string
	Decision                                                orchestrator.Decision
}

// Reader lists a tenant's idle investigations (Postgres, by the tenant's own login; the database clock decides idleness).
type Reader interface {
	Idle(ctx context.Context, tenantID string, idleMS int64, limit int) ([]Candidate, error)
}

// Resumer is the write runner's ResumeWrite.
type Resumer interface {
	ResumeWrite(ctx context.Context, s orchestrator.Session, t orchestrator.Turn, inv orchestrator.Investigation, d orchestrator.Decision) (orchestrator.Result, error)
}

// Config is the pass's settings; LoadConfig reads them and no default exists.
type Config struct {
	IdleMS int64
	Batch  int
}

// Activity is RecoverInvestigations.
type Activity struct{}

// ErrNoDispatch is what NoDispatch answers.
var ErrNoDispatch = errors.New("recovery: recovery never dispatches")

// NoDispatch is the Executor the recovery runner is wired with.
type NoDispatch struct{}

// Execute is a stub.
func (NoDispatch) Execute(context.Context, orchestrator.Session, string) (orchestrator.Receipt, error) {
	return orchestrator.Receipt{}, nil
}

// Setting names.
const (
	EnvIdleMS    = "NOEMA_STM_RECOVERY_IDLE_MS"
	EnvBatchSize = "NOEMA_STM_RECOVERY_BATCH_SIZE"
)

// LoadConfig is a stub.
func LoadConfig(func(string) string) (Config, error) {
	return Config{}, errors.New("recovery: not built")
}

// NewRecoverySession is a stub.
func NewRecoverySession(Candidate) (orchestrator.Session, error) { return orchestrator.Session{}, nil }

// New is a stub.
func New(c Config, r Reader, j orchestrator.Journal, a orchestrator.Admissions, x Resumer) (*Activity, error) {
	if r == nil || j == nil || a == nil || x == nil || c.IdleMS < 1 || c.Batch < 1 {
		return nil, errors.New("recovery: not wired")
	}
	return &Activity{}, nil
}

// Recover is a stub.
func (*Activity) Recover(context.Context, string) (Report, error) { return Report{}, nil }

// StartupPass is a stub.
func (*Activity) StartupPass(context.Context, []string) (map[string]Report, error) { return nil, nil }
