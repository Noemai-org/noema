package trustport

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// Errors of the Recorder adapter (TRU-12).
var (
	ErrRecordRequest = errors.New("trustport: the record request cannot be honoured")
	ErrBasisDigest   = errors.New("trustport: the basis digest is not the builder's")
	ErrPlanHash      = errors.New("trustport: the confirmation was given for another plan")
	ErrCommit        = errors.New("trustport: the commit failed; the outcome is the stored decision's")
)

// TxBeginner opens a transaction as the tenant's own database login.
type TxBeginner interface {
	Begin(ctx context.Context, tenantID string) (pgx.Tx, error)
}

// RecordWriter is recorder.Writer.
type RecordWriter interface {
	Write(ctx context.Context, tx pgx.Tx, r recorder.Request) (recorder.Result, error)
}

// RecordStepFacts is what the composition knows about one write step.
type RecordStepFacts struct {
	TargetVersions []contracts.DecisionGrantV1TargetVersionsItem
	Permission     reconcile.Permission
}

// RecordFacts is what the composition adds to a RecordRequest.
type RecordFacts struct {
	PolicyID        string
	EvidenceSetHash string
	Steps           []RecordStepFacts
}

// RecordFactSource gives the facts of one request.
type RecordFactSource interface {
	Facts(ctx context.Context, r intake.RecordRequest) (RecordFacts, error)
}

// RecorderConfig builds a RecorderAdapter.
type RecorderConfig struct {
	Begin    TxBeginner
	Facts    RecordFactSource
	Keys     *grants.Keys
	Appender *chain.Appender
	Clock    clock.Clock
	IDs      interface {
		ID(prefix string) (string, error)
	}
	Entropy        io.Reader
	GrantLifetime  time.Duration
	RecordLifetime time.Duration
	Escalation     recorder.EscalationSet
}

// RecorderAdapter implements intake.Recorder.
type RecorderAdapter struct {
	begin  TxBeginner
	facts  RecordFactSource
	writer RecordWriter
}

var _ intake.Recorder = (*RecorderAdapter)(nil)

// NewRecorderAdapter builds the adapter.
func NewRecorderAdapter(RecorderConfig) (*RecorderAdapter, error) { return &RecorderAdapter{}, nil }

// Record is not implemented.
func (*RecorderAdapter) Record(context.Context, intake.RecordRequest) (intake.RecordResult, error) {
	return intake.RecordResult{}, errors.New("not implemented")
}
