package reconcilerport

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

var (
	// ErrNoLogin is returned for a tenant that has no Trust login configured, and for a call that names no tenant. It never falls back to another tenant's login.
	ErrNoLogin = errors.New(prefix + "no Trust login is configured for this tenant")
	// ErrStore is returned when the database could not answer. The cause is reachable with errors.Is and is never printed: a driver's error can name the server and the login.
	ErrStore = errors.New(prefix + "the database could not answer")
)

// Beginner begins a transaction of one tenant's own Trust login; *pgxpool.Pool is the real one.
type Beginner interface {
	BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error)
}

// PoolsOf is the logins of cmd/core's per-tenant Trust pools as Beginners. A tenant with no pool has no entry, so it has no login.
func PoolsOf(pools map[string]*pgxpool.Pool) map[string]Beginner {
	out := make(map[string]Beginner, len(pools))
	for tenantID, p := range pools {
		if p != nil {
			out[tenantID] = p
		}
	}
	return out
}

var tenantRe = regexp.MustCompile(`\Aten_[0-9A-Za-z]{2,32}\z`)

// Store reads sealed snapshots over one Trust login per tenant (trust_rw holds SELECT on context_snapshot and evidence_item; the database, through row-level security on the login's tenant, decides
// which rows a connection sees, migrations 0008 and 0020). It implements Snapshots and writes nothing.
type Store struct{ pools map[string]Beginner }

var _ Snapshots = (*Store)(nil)

// NewStore refuses no login at all, a tenant ID that is not one, and a nil login.
func NewStore(pools map[string]Beginner) (*Store, error) {
	if len(pools) == 0 {
		return nil, ErrConfig
	}
	own := make(map[string]Beginner, len(pools))
	for tenantID, p := range pools {
		if !tenantRe.MatchString(tenantID) || p == nil {
			return nil, ErrConfig
		}
		own[tenantID] = p
	}
	return &Store{pools: own}, nil
}

const (
	snapshotSQL = `SELECT tenant_id, snapshot_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at
		FROM context_snapshot WHERE tenant_id = $1 AND snapshot_id = $2`
	itemsSQL = `SELECT evidence_id, payload_ref, payload_hash, source, fact_status, read_at, effective_at, freshness_sla_seconds
		FROM evidence_item WHERE tenant_id = $1 AND evidence_id = ANY ($2)`
)

// Sealed reads the tenant's snapshot and its items in one read-only repeatable-read transaction of the tenant's login. A snapshot that is absent, another tenant's, or named by an ID that is not
// the shape of a snapshot ID is ErrNotFound. The freshness limit of an item is the row's own (freshness_sla_seconds): the stricter limit the Evidence layer applied at read time is not stored, and it
// feeds only the expiry the Reconciler computes, never a classification.
func (s *Store) Sealed(ctx context.Context, tenantID, snapshotID string) (Sealed, error) {
	if s == nil || len(s.pools) == 0 {
		return Sealed{}, ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok {
		return Sealed{}, ErrNoDeadline
	}
	pool, ok := s.pools[tenantID]
	if !ok {
		return Sealed{}, ErrNoLogin
	}
	if !snapshotIDRe.MatchString(snapshotID) {
		return Sealed{}, ErrNotFound
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Sealed{}, &opError{kind: ErrStore, cause: err}
	}
	defer func() { _ = tx.Rollback(ctx) }() // a read-only transaction: nothing to keep; a rollback that fails on a finished context makes the driver drop the connection
	var out Sealed
	err = tx.QueryRow(ctx, snapshotSQL, tenantID, snapshotID).Scan(&out.TenantID, &out.SnapshotID, &out.EvidenceIDs, &out.ReconciliationID, &out.Classification, &out.EvidenceSetHash, &out.SealedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Sealed{}, ErrNotFound
	case err != nil:
		return Sealed{}, &opError{kind: ErrStore, cause: err}
	}
	if out.TenantID != tenantID || out.SnapshotID != snapshotID { // the second lock: row-level security is the first
		return Sealed{}, ErrNotFound
	}
	out.SealedAt = out.SealedAt.UTC()
	if out.Items, err = items(ctx, tx, tenantID, out.EvidenceIDs); err != nil {
		return Sealed{}, err
	}
	return out, nil
}

func items(ctx context.Context, tx pgx.Tx, tenantID string, ids []string) ([]layer.Item, error) {
	rows, err := tx.Query(ctx, itemsSQL, tenantID, ids)
	if err != nil {
		return nil, &opError{kind: ErrStore, cause: err}
	}
	defer rows.Close()
	var out []layer.Item
	for rows.Next() {
		var (
			it     layer.Item
			status string
			sla    int64
			read   time.Time
			eff    time.Time
		)
		if err := rows.Scan(&it.EvidenceID, &it.PayloadRef, &it.PayloadHash, &it.Source, &status, &read, &eff, &sla); err != nil {
			return nil, &opError{kind: ErrStore, cause: err}
		}
		it.Status, it.ReadAt, it.EffectiveAt, it.EffectiveFreshnessSeconds = contracts.EnumsV1FactStatus(status), read.UTC(), eff.UTC(), sla
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, &opError{kind: ErrStore, cause: err}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EvidenceID < out[j].EvidenceID }) // bytewise, as the sealer sorts: the database's collation may differ
	return out, nil
}
