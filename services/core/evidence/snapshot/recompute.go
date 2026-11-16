package snapshot

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The status of one recomputed snapshot.
const (
	StatusOK              = "OK"
	StatusMismatch        = "MISMATCH"
	StatusMissingEvidence = "MISSING_EVIDENCE"
)

// verifyLogin is the read-only login of the nightly recompute (D9; deploy/compose/postgres/init.sh).
const verifyLogin = "evidence_verify"

// Result is the outcome of recomputing one snapshot. It carries the tenant, the snapshot and digests, and nothing else: no evidence id, no payload,
// no correlation (D9; the leak scanner of noema#222 holds the output to it).
type Result struct {
	TenantID       string `json:"tenant_id"`
	SnapshotID     string `json:"snapshot_id"`
	Status         string `json:"status"`
	StoredHash     string `json:"stored_hash"`
	RecomputedHash string `json:"recomputed_hash"`
}

// AnyFailure reports whether any snapshot is not OK. A failure is a finding for the operator: this package changes nothing.
func AnyFailure(rs []Result) bool {
	for _, r := range rs {
		if r.Status != StatusOK {
			return true
		}
	}
	return false
}

// ErrWrongLogin is returned when the pool does not log in as evidence_verify. Another login sees one tenant (row-level security) or nothing, so
// its result would read as a check of every tenant and be one of a few; the recompute fails closed instead.
var ErrWrongLogin = errors.New("snapshot: the recompute must run as the evidence_verify login")

// Recompute recomputes the evidence_set_hash of every stored snapshot from the stored evidence items and compares it with the stored hash
// (REQ-CTX-004; the nightly job of context-snapshot.v1). pool must log in as evidence_verify. A snapshot whose evidence item is gone is
// MISSING_EVIDENCE; one whose recomputed hash differs is MISMATCH. The result is sorted by tenant and snapshot.
func Recompute(ctx context.Context, pool *pgxpool.Pool) ([]Result, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("snapshot: the context needs a deadline")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("snapshot: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var who string
	if err := tx.QueryRow(ctx, `SELECT session_user::text`).Scan(&who); err != nil {
		return nil, fmt.Errorf("snapshot: who am I: %w", err)
	}
	if who != verifyLogin {
		return nil, ErrWrongLogin
	}
	type row struct {
		tenant, id, hash string
		ids              []string
	}
	rows, err := tx.Query(ctx, `SELECT tenant_id, snapshot_id, evidence_set_hash, evidence_ids FROM context_snapshot ORDER BY tenant_id, snapshot_id`)
	if err != nil {
		return nil, fmt.Errorf("snapshot: read the snapshots: %w", err)
	}
	snaps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var s row
		return s, r.Scan(&s.tenant, &s.id, &s.hash, &s.ids)
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot: read the snapshots: %w", err)
	}
	out := make([]Result, 0, len(snaps))
	for _, s := range snaps {
		items, err := tx.Query(ctx, `SELECT evidence_id, payload_hash FROM evidence_item WHERE tenant_id = $1 AND evidence_id = ANY ($2)`, s.tenant, s.ids)
		if err != nil {
			return nil, fmt.Errorf("snapshot: read the evidence items of %s: %w", s.id, err)
		}
		pairs, err := pgx.CollectRows(items, func(r pgx.CollectableRow) (Pair, error) {
			var p Pair
			return p, r.Scan(&p.EvidenceID, &p.PayloadHash)
		})
		if err != nil {
			return nil, fmt.Errorf("snapshot: read the evidence items of %s: %w", s.id, err)
		}
		res := Result{TenantID: s.tenant, SnapshotID: s.id, StoredHash: s.hash}
		if len(pairs) != len(s.ids) {
			res.Status = StatusMissingEvidence
			out = append(out, res)
			continue
		}
		res.RecomputedHash, err = SetHash(pairs)
		switch {
		case err != nil:
			res.Status, res.RecomputedHash = StatusMismatch, ""
		case res.RecomputedHash != s.hash:
			res.Status = StatusMismatch
		default:
			res.Status = StatusOK
		}
		out = append(out, res)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TenantID != out[j].TenantID {
			return out[i].TenantID < out[j].TenantID
		}
		return out[i].SnapshotID < out[j].SnapshotID
	})
	return out, nil
}
