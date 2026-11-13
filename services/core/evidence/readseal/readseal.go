package readseal

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

// Intents reads one admitted intent; intentstore.Reader is the real one.
type Intents interface {
	Get(ctx context.Context, intentID string, version int) (intentstore.Admitted, error)
}

// Transactions reads one transaction through the read ports and records the evidence; portread.Reader is the real one.
type Transactions interface {
	ReadTransaction(ctx context.Context, scope adapters.Scope, correlationID, transactionRef string) ([]layer.Item, error)
}

// Classified is the state reconciliation of one evidence set, as the snapshot records it.
type Classified struct{ ReconciliationID, Classification string }

// Reconciler classifies the evidence items just read. It has no default: an adapter without one is not wired.
type Reconciler interface {
	Classify(ctx context.Context, tenantID string, items []layer.Item) (Classified, error)
}

// Sealer seals a snapshot in the caller's transaction; snapshot.Sealer is the real one.
type Sealer interface {
	Seal(ctx context.Context, tx pgx.Tx, in snapshot.Input) (snapshot.Sealed, error)
}

// TxSource begins a transaction of the tenant's Trust login; *pgxpool.Pool is the real one.
type TxSource interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Errors of the adapter. Callers compare with errors.Is.
var (
	ErrNotWired      = errors.New("readseal: the adapter is not fully wired")
	ErrIntentRef     = errors.New("readseal: the intent reference is not intent_id@version")
	ErrTenant        = errors.New("readseal: the session or the intent is not of this adapter's tenant")
	ErrNoTransaction = errors.New("readseal: the intent names no resolved transaction")
	ErrNoDeadline    = errors.New("readseal: the context needs a deadline")
)

// Adapter implements orchestrator.Evidence for one tenant.
type Adapter struct {
	TenantID     string
	Intents      Intents
	Transactions Transactions
	Reconciler   Reconciler
	Sealer       Sealer
	Tx           TxSource
	Timeout      time.Duration // the deadline of the seal transaction; from configuration, zero refuses every read (AGENT-16)
}

// Read is not built yet.
func (a *Adapter) Read(context.Context, orchestrator.Session, string) (orchestrator.EvidenceRef, error) {
	return orchestrator.EvidenceRef{}, errors.New("readseal: not built")
}

var _ orchestrator.Evidence = (*Adapter)(nil)
