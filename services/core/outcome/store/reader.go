package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/outcome/request"
)

// Reader is the read-only Record reader of LANE-5 slice 4 (A675, A692; interim, relayed; REQ-VER-001, REQ-SEC-004; INV-04, INV-12, AGENT-13). It reads, as the tenant's outcome login, what
// Execution wrote for one grant: the receipt with its signed document and step_params, the grant's capability, plan hash and intent hash, and the decision's intent. It writes nothing.
type Reader struct {
	Pool *pgxpool.Pool // the tenant's outcome_rw login
}

var _ request.Reader = Reader{}

// Load returns the facts of the tenant's grant, or request.ErrNotFound.
func (r Reader) Load(ctx context.Context, tenantID, grantID string) (request.Facts, error) {
	return request.Facts{}, errors.New(errPrefix + "reader not built")
}
