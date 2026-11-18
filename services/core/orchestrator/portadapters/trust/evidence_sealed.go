package trustport

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// SealedItem is one evidence item of a sealed snapshot as the database holds it.
type SealedItem struct {
	EvidenceID    string
	CorrelationID string
	Source        string
	ReadAt        time.Time
	EffectiveAt   time.Time
	SLASeconds    int64
	Status        string
	PayloadHash   string
}

// SealedIntent is the latest admitted intent of the snapshot's correlation.
type SealedIntent struct {
	IntentID   string
	Version    int
	IntentNode string
	AdmittedAt time.Time
}

// SealedSnapshot is a sealed snapshot with its items and intent.
type SealedSnapshot struct {
	TenantID        string
	SnapshotID      string
	CorrelationID   string
	EvidenceIDs     []string
	EvidenceSetHash string
	Items           []SealedItem
	Intent          SealedIntent
}

// SnapshotReader reads one sealed snapshot of a tenant.
type SnapshotReader interface {
	Sealed(ctx context.Context, tenantID, snapshotID string) (SealedSnapshot, error)
}

// SnapshotTx begins a transaction of one tenant's Trust login; *pgxpool.Pool is the real one.
type SnapshotTx interface {
	BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error)
}

// SnapshotLogins gives the transaction source of a tenant's own Trust login.
type SnapshotLogins interface {
	TxFor(tenantID string) (SnapshotTx, error)
}

// SealedSnapshots implements SnapshotReader over the tenant's Trust login.
type SealedSnapshots struct {
	logins  SnapshotLogins
	timeout time.Duration
}

// NewSealedSnapshots builds the reader.
func NewSealedSnapshots(logins SnapshotLogins, timeout time.Duration) (SealedSnapshots, error) {
	return SealedSnapshots{logins: logins, timeout: timeout}, nil
}

// Sealed reads the snapshot.
func (s SealedSnapshots) Sealed(context.Context, string, string) (SealedSnapshot, error) {
	return SealedSnapshot{}, nil
}
