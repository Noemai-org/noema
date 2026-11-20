package intakesources

import (
	"context"
	"time"
)

// DBSnapshotItems implements SnapshotItems over the tenant's own Trust login.
type DBSnapshotItems struct{ r reader }

var _ SnapshotItems = (*DBSnapshotItems)(nil)

// NewSnapshotItems builds the reader.
func NewSnapshotItems(logins Logins, timeout time.Duration) (*DBSnapshotItems, error) {
	return &DBSnapshotItems{}, nil
}

// Items reads the items of the sealed snapshot.
func (s *DBSnapshotItems) Items(ctx context.Context, tenantID, snapshotID string) ([]SnapshotItem, error) {
	return nil, ErrConfig
}
