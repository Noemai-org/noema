package snapshot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// ErrEmptySet is returned when asked to seal no evidence. Trust never issues a grant over an empty evidence set (INV-17, DEC-2026-10-16 C1), so
// the empty set is never sealed, whatever its hash is.
var ErrEmptySet = errors.New("snapshot: a snapshot needs at least one evidence item (INV-17)")

const (
	schemaVersion = "1.0"
	emitter       = "core-trust"
	eventSealed   = "CONTEXT_SNAPSHOT_SEALED"
)

// IDSource makes snapshot IDs (`evs_<ULID>`, PERD 32.3).
type IDSource interface {
	ID(prefix string) (string, error)
}

// Input is what Trust asks the sealer to seal: the evidence items it read for one evaluation and the state reconciliation that classified them.
type Input struct {
	TenantID         string
	CorrelationID    string
	EvidenceIDs      []string
	ReconciliationID string
	Classification   string
}

// Reconciliation is the reconciliation block of context-snapshot.v1.
type Reconciliation struct {
	ReconciliationID string `json:"reconciliation_id"`
	Classification   string `json:"classification"`
}

// Sealed is a context-snapshot.v1 document.
type Sealed struct {
	SchemaVersion   string         `json:"schema_version"`
	TenantID        string         `json:"tenant_id"`
	CorrelationID   string         `json:"correlation_id"`
	SnapshotID      string         `json:"snapshot_id"`
	EvidenceIDs     []string       `json:"evidence_ids"`
	Reconciliation  Reconciliation `json:"reconciliation"`
	EvidenceSetHash string         `json:"evidence_set_hash"`
	SealedAt        string         `json:"sealed_at"`
}

// Sealer seals a snapshot in the caller's transaction (REQ-CTX-004, REQ-AUD-001).
type Sealer struct {
	Rec   *chain.Appender
	Clock clock.Clock
	IDs   IDSource
}

// Seal sorts the evidence IDs, reads their payload hashes, computes the evidence_set_hash, appends CONTEXT_SNAPSHOT_SEALED to the Record and
// stores the snapshot, all in tx, which must come from a pool that logs in as the tenant's Trust login. The database recomputes the hash from the
// stored items and refuses a set that is empty, unsorted, repeated or not the tenant's own (migration 0020); this function refuses the
// same early, with a typed error for the empty set. On any error the caller rolls tx back and nothing is left: the Record event and the row stand or
// fall together.
func (s *Sealer) Seal(ctx context.Context, tx pgx.Tx, in Input) (Sealed, error) {
	if len(in.EvidenceIDs) == 0 {
		return Sealed{}, ErrEmptySet
	}
	ids := append([]string(nil), in.EvidenceIDs...)
	sort.Strings(ids) // bytewise, which is UTF-8 order
	for i, id := range ids {
		if id == "" {
			return Sealed{}, errors.New("snapshot: an evidence_id is empty")
		}
		if i > 0 && ids[i-1] == id {
			return Sealed{}, fmt.Errorf("snapshot: evidence_id %q appears more than once", id)
		}
	}
	rows, err := tx.Query(ctx, `SELECT evidence_id, payload_hash FROM evidence_item WHERE tenant_id = $1 AND evidence_id = ANY ($2)`, in.TenantID, ids)
	if err != nil {
		return Sealed{}, fmt.Errorf("snapshot: read the evidence items: %w", err)
	}
	pairs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Pair, error) {
		var p Pair
		return p, r.Scan(&p.EvidenceID, &p.PayloadHash)
	})
	if err != nil {
		return Sealed{}, fmt.Errorf("snapshot: read the evidence items: %w", err)
	}
	if len(pairs) != len(ids) {
		return Sealed{}, fmt.Errorf("snapshot: %d of %d evidence ids name an evidence item of tenant %s", len(pairs), len(ids), in.TenantID)
	}
	hash, err := SetHash(pairs)
	if err != nil {
		return Sealed{}, err
	}
	id, err := s.IDs.ID("evs")
	if err != nil {
		return Sealed{}, fmt.Errorf("snapshot: snapshot id: %w", err)
	}
	at := s.Clock.Now().UTC().Truncate(time.Millisecond)
	doc := Sealed{SchemaVersion: schemaVersion, TenantID: in.TenantID, CorrelationID: in.CorrelationID, SnapshotID: id, EvidenceIDs: ids,
		Reconciliation: Reconciliation{ReconciliationID: in.ReconciliationID, Classification: in.Classification}, EvidenceSetHash: hash,
		SealedAt: at.Format("2006-01-02T15:04:05.000Z")}
	if _, err := s.Rec.Append(ctx, tx, chain.Event{TenantID: in.TenantID, CorrelationID: in.CorrelationID, WrittenBy: emitter, Actor: emitter,
		EventType: eventSealed, ObjectRef: id, Payload: doc}); err != nil {
		return Sealed{}, fmt.Errorf("snapshot: append %s: %w", eventSealed, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, in.TenantID, in.CorrelationID, ids, in.ReconciliationID, in.Classification, hash, at); err != nil {
		return Sealed{}, fmt.Errorf("snapshot: store %s: %w", id, err)
	}
	return doc, nil
}
