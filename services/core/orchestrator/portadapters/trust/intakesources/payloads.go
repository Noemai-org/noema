package intakesources

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// payloadSQL reads the stored JSON of one payload. A payload over the bound is not read out of the database: the answer is NULL, which is the bound's refusal.
const payloadSQL = `SELECT CASE WHEN octet_length(json::text) <= $3 THEN json::text END FROM evidence_payload WHERE tenant_id = $1 AND payload_ref = $2`

// PayloadStore implements payloadread.PayloadReader over the tenant's own Trust login (trust_rw; migration 0008), as cmd/core's reader does. It reads only. The payload it returns is
// not trusted: payloadread.Reader verifies it against the item's hash before anything is read from it.
type PayloadStore struct {
	r        reader
	maxBytes int64
}

var _ payloadread.PayloadReader = (*PayloadStore)(nil)

// NewPayloadStore builds the reader. A missing login source, a timeout below one or a bound below one byte is ErrConfig. The bound is the largest payload, in bytes of its JSON text; it
// comes from configuration (NOEMA_PAYLOAD_MAX_BYTES) and has no default (AGENT-16).
func NewPayloadStore(logins Logins, timeout time.Duration, maxBytes int64) (*PayloadStore, error) {
	r, err := newReader(logins, timeout)
	if err != nil {
		return nil, err
	}
	if maxBytes < 1 {
		return nil, ErrConfig
	}
	return &PayloadStore{r: r, maxBytes: maxBytes}, nil
}

// Read gives the JSON of the tenant's payload under ref. A payload that does not exist, or that another tenant owns, is ErrNoPayload; one over the bound is ErrPayloadTooLarge and is not
// read into memory.
func (p *PayloadStore) Read(ctx context.Context, tenantID, ref string) ([]byte, error) {
	if p == nil {
		return nil, ErrConfig
	}
	if !chain.ValidTenantID(tenantID) || ref == "" {
		return nil, fmt.Errorf("%w: the tenant or the payload reference is outside its shape", ErrQuery)
	}
	var doc *string
	if err := p.r.scan(ctx, tenantID, payloadSQL, []any{tenantID, ref, p.maxBytes}, &doc); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNoPayload
		}
		return nil, err
	}
	if doc == nil {
		return nil, ErrPayloadTooLarge
	}
	return []byte(*doc), nil
}
