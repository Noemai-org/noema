package trustport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// What the Evidence adapter (A329) reads about one sealed snapshot, and the reader that reads it as the tenant's own Trust login (trust_rw). The sealed snapshot is
// context_snapshot (migration 0020), its items are evidence_item (0008) and the intent is the latest admitted_intent of the snapshot's correlation (0026). Nothing is read
// from evidence_payload: the decision needs the item's own columns and never its body.

// SealedItem is one evidence item of a sealed snapshot as the database holds it. Status is the stored text, checked against the contract's enum by the adapter.
type SealedItem struct {
	EvidenceID    string
	CorrelationID string
	Source        string
	// SourceRecordID is the record of the source the item was read from (`source_record_id`, required by the evidence item contract): with the source and the payload hash it is the
	// item's content, the part of it that does not depend on the evidence ID minted for the read (A365).
	SourceRecordID string
	// PayloadRef is the reference of the item's stored payload (`payload_ref`), which the facts source reads as the tenant and verifies against PayloadHash (A365 refined).
	PayloadRef  string
	ReadAt      time.Time
	EffectiveAt time.Time
	// SLASeconds is the source's own freshness limit (`freshness_sla_seconds`); the profile's cap is not stored on the item.
	SLASeconds  int64
	Status      string
	PayloadHash string
}

// SealedIntent is the latest admitted intent of the snapshot's correlation. The sealed snapshot does not name its intent, so this is what the adapter has to go on. The
// intent store keeps one chain per investigation, so the highest version is the latest.
type SealedIntent struct {
	IntentID   string
	Version    int
	IntentNode string
	AdmittedAt time.Time
}

// SealedSnapshot is a sealed snapshot with its items and the latest intent of its correlation. Intent is the zero value when the correlation has none.
type SealedSnapshot struct {
	TenantID        string
	SnapshotID      string
	CorrelationID   string
	EvidenceIDs     []string
	EvidenceSetHash string
	Items           []SealedItem
	Intent          SealedIntent
}

// SnapshotReader reads one sealed snapshot of a tenant. A snapshot that does not exist, or that another tenant owns, is ErrSnapshotNotFound: the same answer for both.
type SnapshotReader interface {
	Sealed(ctx context.Context, tenantID, snapshotID string) (SealedSnapshot, error)
}

// SnapshotTx begins a transaction of one tenant's Trust login; *pgxpool.Pool is the real one.
type SnapshotTx interface {
	BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error)
}

// SnapshotLogins gives the transaction source of a tenant's own Trust login. A tenant with no login is an error: there is no fallback to another tenant's login.
type SnapshotLogins interface {
	TxFor(tenantID string) (SnapshotTx, error)
}

// SealedSnapshots implements SnapshotReader over the tenant's Trust login. The database, not this code, binds every read to the tenant (row-level security through the login's
// own mapping); the queries also name the tenant.
type SealedSnapshots struct {
	logins  SnapshotLogins
	timeout time.Duration
}

var _ SnapshotReader = SealedSnapshots{}

// NewSealedSnapshots builds the reader. The timeout comes from configuration (NOEMA_TRUST_DB_TIMEOUT_MS); a missing login source or a timeout below one is ErrConfig (AGENT-16).
func NewSealedSnapshots(logins SnapshotLogins, timeout time.Duration) (SealedSnapshots, error) {
	if logins == nil || timeout <= 0 {
		return SealedSnapshots{}, ErrConfig
	}
	return SealedSnapshots{logins: logins, timeout: timeout}, nil
}

// failure is an error with a fixed text and a cause that errors.Is can still find: a database or login error can name a server, a login, a tenant or a row, so its text is
// never shown (AGENT-13).
type failure struct{ kind, cause error }

func (f *failure) Error() string { return f.kind.Error() }

func (f *failure) Unwrap() []error {
	if f.cause == nil {
		return []error{f.kind}
	}
	return []error{f.kind, f.cause}
}

func wrapped(kind, cause error) error { return &failure{kind: kind, cause: cause} }

const (
	selectSnapshot = `SELECT snapshot_id, tenant_id, correlation_id, evidence_ids, evidence_set_hash FROM context_snapshot WHERE tenant_id = $1 AND snapshot_id = $2`
	selectItems    = `SELECT evidence_id, correlation_id, source, read_at, effective_at, freshness_sla_seconds::bigint, fact_status, payload_hash, source_record_id, payload_ref
		FROM evidence_item WHERE tenant_id = $1 AND evidence_id = ANY ($2)`
	selectIntent = `SELECT intent_id, version, intent_node, created_at FROM admitted_intent WHERE tenant_id = $1 AND correlation_id = $2 ORDER BY version DESC LIMIT 1`
)

// Sealed reads the snapshot, its items and its correlation's latest intent in one read-only repeatable-read transaction of the tenant's own login, so the three are one view,
// and ends it by rollback. A snapshot the login cannot see is ErrSnapshotNotFound.
func (s SealedSnapshots) Sealed(ctx context.Context, tenantID, snapshotID string) (SealedSnapshot, error) {
	if s.logins == nil || s.timeout <= 0 {
		return SealedSnapshot{}, ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok {
		return SealedSnapshot{}, ErrEvidenceNoDeadline
	}
	src, err := s.logins.TxFor(tenantID)
	if err != nil {
		return SealedSnapshot{}, wrapped(ErrSnapshotStore, err)
	}
	if src == nil {
		return SealedSnapshot{}, wrapped(ErrSnapshotStore, nil)
	}
	qctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	tx, err := src.BeginTx(qctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return SealedSnapshot{}, wrapped(ErrSnapshotStore, err)
	}
	defer func() {
		rctx, stop := context.WithTimeout(context.WithoutCancel(ctx), s.timeout) // the rollback must run when the read's own deadline has passed
		defer stop()
		_ = tx.Rollback(rctx) // a read-only transaction has nothing to lose: a failed rollback only returns the connection to the pool late
	}()
	out, err := read(qctx, tx, tenantID, snapshotID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return SealedSnapshot{}, ErrSnapshotNotFound
	case err != nil:
		return SealedSnapshot{}, wrapped(ErrSnapshotStore, err)
	}
	return out, nil
}

func read(ctx context.Context, tx pgx.Tx, tenantID, snapshotID string) (SealedSnapshot, error) {
	var out SealedSnapshot
	if err := tx.QueryRow(ctx, selectSnapshot, tenantID, snapshotID).Scan(&out.SnapshotID, &out.TenantID, &out.CorrelationID, &out.EvidenceIDs, &out.EvidenceSetHash); err != nil {
		return SealedSnapshot{}, err
	}
	rows, err := tx.Query(ctx, selectItems, tenantID, out.EvidenceIDs)
	if err != nil {
		return SealedSnapshot{}, fmt.Errorf("items: %w", err)
	}
	out.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (SealedItem, error) {
		var it SealedItem
		return it, r.Scan(&it.EvidenceID, &it.CorrelationID, &it.Source, &it.ReadAt, &it.EffectiveAt, &it.SLASeconds, &it.Status, &it.PayloadHash, &it.SourceRecordID, &it.PayloadRef)
	})
	if err != nil {
		return SealedSnapshot{}, fmt.Errorf("items: %w", err)
	}
	err = tx.QueryRow(ctx, selectIntent, tenantID, out.CorrelationID).Scan(&out.Intent.IntentID, &out.Intent.Version, &out.Intent.IntentNode, &out.Intent.AdmittedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		out.Intent = SealedIntent{} // the correlation has no admitted intent: the adapter refuses to attribute the snapshot
	case err != nil:
		return SealedSnapshot{}, fmt.Errorf("intent: %w", err)
	}
	return out, nil
}
