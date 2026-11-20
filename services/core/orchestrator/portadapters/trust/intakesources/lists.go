package intakesources

import (
	"context"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// SnapshotItem is one evidence item of a sealed snapshot, as stored.
type SnapshotItem struct {
	EvidenceID, Source, ResourceScope, Status, PayloadRef, PayloadHash string
}

// SnapshotItems reads the items of one sealed snapshot of a tenant.
type SnapshotItems interface {
	Items(ctx context.Context, tenantID, snapshotID string) ([]SnapshotItem, error)
}

// ListMapper reads the rows of a list entity from a stored payload; *mapping.Mapping is the real one.
type ListMapper interface {
	ListRows(source, entity string, payload map[string]any) ([]predicate.Row, error)
}

// ListSpec says which item of the sealed snapshot holds one list entity.
type ListSpec struct{ Source, ScopePrefix string }

// EvidenceLists is what the readers of list entities need.
type EvidenceLists struct {
	Mapping  ListMapper
	Items    SnapshotItems
	Payloads payloadread.PayloadReader
	Entities map[string]ListSpec
}
