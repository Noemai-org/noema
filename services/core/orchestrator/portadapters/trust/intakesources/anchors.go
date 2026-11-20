package intakesources

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

// AnchorEvent says where an anchor event is in the sealed snapshot's lists: the entity and the field that holds its time.
type AnchorEvent struct{ Entity, Field string }

// Anchors implements intakeadapters.Anchors over the sealed snapshot's lists.
type Anchors struct{}

var _ intakeadapters.Anchors = (*Anchors)(nil)

// NewAnchors builds the reader.
func NewAnchors(lists EvidenceLists, events map[string]AnchorEvent) (*Anchors, error) {
	return &Anchors{}, nil
}

// AnchorTime gives the time of the anchor event for the sealed snapshot.
func (a *Anchors) AnchorTime(ctx context.Context, tenantID, snapshotID, anchorEvent string) (int64, bool, error) {
	return 0, false, ErrNoSource
}
