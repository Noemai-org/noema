package intakesources

import (
	"context"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
)

// PayloadStore implements payloadread.PayloadReader over the tenant's own Trust login.
type PayloadStore struct{ r reader }

var _ payloadread.PayloadReader = (*PayloadStore)(nil)

// NewPayloadStore builds the reader; maxBytes is the bound of a payload's JSON text.
func NewPayloadStore(logins Logins, timeout time.Duration, maxBytes int64) (*PayloadStore, error) {
	return &PayloadStore{}, nil
}

// Read gives the JSON of the tenant's payload under ref.
func (p *PayloadStore) Read(ctx context.Context, tenantID, ref string) ([]byte, error) {
	return nil, ErrConfig
}
