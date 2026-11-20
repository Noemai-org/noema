package intakesources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
)

// snapshotSQL reads a sealed snapshot and its items in one statement, so the two are one view: the snapshot is the row of context_snapshot (a snapshot that is not sealed has none), and its
// items are the evidence_item rows of the tenant whose IDs it names. The database binds the read to the tenant (row-level security) and the statement names it too.
const snapshotSQL = `SELECT s.correlation_id, s.evidence_ids, s.evidence_set_hash,
	COALESCE((SELECT jsonb_agg(jsonb_build_object('evidence_id', i.evidence_id, 'correlation_id', i.correlation_id, 'source', i.source, 'resource_scope', i.resource_scope,
		'status', i.fact_status, 'payload_ref', i.payload_ref, 'payload_hash', i.payload_hash))
		FROM evidence_item i WHERE i.tenant_id = s.tenant_id AND i.evidence_id = ANY (s.evidence_ids)), '[]'::jsonb)::text
	FROM context_snapshot s WHERE s.tenant_id = $1 AND s.snapshot_id = $2`

// DBSnapshotItems implements SnapshotItems over the tenant's own Trust login (trust_rw; migrations 0008 and 0020). It reads only.
type DBSnapshotItems struct{ r reader }

var _ SnapshotItems = (*DBSnapshotItems)(nil)

// NewSnapshotItems builds the reader. A missing login source or a timeout below one is ErrConfig. The timeout comes from configuration (NOEMA_TRUST_DB_TIMEOUT_MS).
func NewSnapshotItems(logins Logins, timeout time.Duration) (*DBSnapshotItems, error) {
	r, err := newReader(logins, timeout)
	if err != nil {
		return nil, err
	}
	return &DBSnapshotItems{r: r}, nil
}

type storedItem struct {
	EvidenceID    string `json:"evidence_id"`
	CorrelationID string `json:"correlation_id"`
	Source        string `json:"source"`
	ResourceScope string `json:"resource_scope"`
	Status        string `json:"status"`
	PayloadRef    string `json:"payload_ref"`
	PayloadHash   string `json:"payload_hash"`
}

// Items gives the items of the sealed snapshot, in the order of the sealed IDs, only when the stored snapshot is exactly what was sealed: every ID names one item of the snapshot's own
// correlation, none is missing or repeated, and the hash over the (ID, payload hash) pairs is the one stored (INV-17). A snapshot that does not exist, that is not sealed or that another
// tenant owns is ErrNoSnapshot; one that does not verify is ErrSnapshotIntegrity.
func (s *DBSnapshotItems) Items(ctx context.Context, tenantID, snapshotID string) ([]SnapshotItem, error) {
	if s == nil {
		return nil, ErrConfig
	}
	if err := checkSnapshot(tenantID, snapshotID); err != nil {
		return nil, err
	}
	var (
		correlation, setHash, doc string
		ids                       []string
	)
	if err := s.r.scan(ctx, tenantID, snapshotSQL, []any{tenantID, snapshotID}, &correlation, &ids, &setHash, &doc); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNoSnapshot
		}
		return nil, err
	}
	var stored []storedItem
	if json.Unmarshal([]byte(doc), &stored) != nil {
		return nil, fmt.Errorf("%w: the items do not decode", ErrSnapshotIntegrity)
	}
	return verified(correlation, setHash, ids, stored)
}

func verified(correlation, setHash string, ids []string, stored []storedItem) ([]SnapshotItem, error) {
	bad := func(why string) ([]SnapshotItem, error) { return nil, fmt.Errorf("%w: %s", ErrSnapshotIntegrity, why) }
	if correlation == "" || len(ids) == 0 || len(stored) != len(ids) {
		return bad("the items are not the sealed ones")
	}
	byID := make(map[string]storedItem, len(stored))
	for _, it := range stored {
		if _, dup := byID[it.EvidenceID]; dup || it.EvidenceID == "" || it.CorrelationID != correlation {
			return bad("an item is repeated or is of another case")
		}
		byID[it.EvidenceID] = it
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	out := make([]SnapshotItem, 0, len(sorted))
	pairs := make([]snapshot.Pair, 0, len(sorted))
	for _, id := range sorted {
		it, ok := byID[id]
		if !ok {
			return bad("an item is missing")
		}
		out = append(out, SnapshotItem{EvidenceID: id, Source: it.Source, ResourceScope: it.ResourceScope, Status: it.Status, PayloadRef: it.PayloadRef, PayloadHash: it.PayloadHash})
		pairs = append(pairs, snapshot.Pair{EvidenceID: id, PayloadHash: it.PayloadHash})
	}
	if h, err := snapshot.SetHash(pairs); err != nil || h != setHash {
		return bad("the set hash does not verify")
	}
	return out, nil
}
