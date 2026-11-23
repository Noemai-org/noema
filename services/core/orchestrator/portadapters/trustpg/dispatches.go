package trustpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const eventDispatchAttempted = "DISPATCH_ATTEMPTED"

// TxBeginner is a pool of the tenant's record_read login. The composition adapts the Record reader to RecordDBs: this package does not import the reconstruction, which only
// cmd/core and record/admission may (REQ-AUD-003).
type TxBeginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// RecordDBs gives each tenant its own record_read pool, or an error. It never gives another tenant's.
type RecordDBs interface {
	For(tenantID string) (TxBeginner, error)
}

// KeyDispatches implements trustport.KeyDispatches. The database is the authority on the dispatch budget (A153): its guard counts the DISPATCH_ATTEMPTED events of the Record for the
// key (tenant, correlation, capability, target) when a grant is spent. Trust may not call that function (execution_rw alone holds EXECUTE), so this reader counts the same events the
// same way from the tenant's own record_read login: in one read-only REPEATABLE READ transaction, the chain verified from genesis before it is believed (REQ-AUD-003). Like the
// guard, an event whose key cannot be read counts against every key of its correlation, so a doubtful Record never shows more room than there is.
type KeyDispatches struct{ dbs RecordDBs }

var _ trustport.KeyDispatches = (*KeyDispatches)(nil)

// NewKeyDispatches builds the reader over the Record reader's per-tenant pools. None is ErrConfig.
func NewKeyDispatches(dbs RecordDBs) (*KeyDispatches, error) {
	if dbs == nil {
		return nil, ErrConfig
	}
	return &KeyDispatches{dbs: dbs}, nil
}

// Used counts the dispatches of the key. A key with an empty part, a missing reader or a chain that does not verify is an error, never zero.
func (k *KeyDispatches) Used(ctx context.Context, key intake.DispatchKey) (int, error) {
	if k == nil || k.dbs == nil {
		return 0, ErrConfig
	}
	if key.TenantID == "" || key.CorrelationID == "" || key.CapabilityID == "" || key.TargetRef == "" {
		return 0, ErrBadKey
	}
	if _, ok := ctx.Deadline(); !ok {
		return 0, fmt.Errorf("%w: the context needs a deadline", ErrConfig)
	}
	db, err := k.dbs.For(key.TenantID)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrNoPool, err)
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return 0, errors.New("trustpg: the Record read failed")
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := chain.ReadChain(ctx, tx)
	if err != nil {
		return 0, errors.New("trustpg: the Record read failed")
	}
	if _, err := chain.VerifyRows(key.TenantID, rows); err != nil {
		return 0, fmt.Errorf("trustpg: the Record does not verify: %w", err)
	}
	n := 0
	for _, r := range rows {
		if r.EventType == eventDispatchAttempted && r.CorrelationID == key.CorrelationID && countsAgainst(r.Payload, key) {
			n++
		}
	}
	return n, nil
}

// countsAgainst is the rule of dispatch_budget_count (migration 0023): the payload names this key's capability and target, or it is not an object with both.
func countsAgainst(payload *string, key intake.DispatchKey) bool {
	if payload == nil {
		return true
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(*payload), &p); err != nil || p == nil {
		return true
	}
	c, _ := p["capability"].(string)
	t, _ := p["target_ref"].(string)
	if c == "" || t == "" {
		return true
	}
	return c == key.CapabilityID && t == key.TargetRef
}
