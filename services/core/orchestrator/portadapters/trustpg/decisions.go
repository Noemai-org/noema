package trustpg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

// Decisions reads the ALLOW decision of a correlation for the recovery pass (A696; REQ-RCV-003, REQ-SEC-004; INV-03, INV-12): the decision, its grants and the intent reference of the
// admitted intent it was made on, as the tenant's own trust_rw login under forced row-level security. It is read-only and decides nothing: it names what Trust recorded, and the write
// runner's Bindings then prove it against the Record before anything is moved. The orchestrator's own login cannot read these tables (BD-05), so recovery asks Trust's side through here.
type Decisions struct {
	pools   Pools
	timeout time.Duration
}

// NewDecisions builds the reader. No pool or a timeout below 1ns is ErrConfig.
func NewDecisions(p Pools, timeout time.Duration) (*Decisions, error) {
	if err := p.usable(timeout); err != nil {
		return nil, err
	}
	return &Decisions{pools: p, timeout: timeout}, nil
}

// allowSQL picks the newest ALLOW decision of the correlation first, then finds its intent reference from the admitted intent whose id and hash the decision names (the record carries
// the id and the hash and no version, and the hash belongs to exactly one version). The tenant is in the WHERE and, under forced row-level security, in the login: a wrong pool and a
// wrong argument each stay harmless alone.
const allowSQL = `WITH d AS (
  SELECT decision_id, intent_hash, COALESCE(json ->> 'intent_id', '') AS intent_id FROM decision_record
  WHERE tenant_id = $1 AND outcome = 'ALLOW' AND json ->> 'correlation_id' = $2 ORDER BY created_at DESC, decision_id DESC LIMIT 1)
SELECT d.decision_id, d.intent_id, a.version FROM d LEFT JOIN admitted_intent a ON a.tenant_id = $1 AND a.intent_id = d.intent_id AND a.intent_hash = d.intent_hash`

const allowGrantsSQL = `SELECT grant_id FROM decision_grant WHERE tenant_id = $1 AND decision_id = $2 ORDER BY grant_id`

var errAllowRead = errors.New("trustpg: the decision read failed") // the driver's error can name the server and the login

// AllowOf returns the newest ALLOW decision the Record holds for the correlation, with its grant ids and its intent reference (empty when no admitted intent matches, which no Binding
// can match). None (no ALLOW, or another tenant's: the same answer) is trustport.ErrNoRecord. When several ALLOW decisions share a correlation, the newest is the one in flight; the
// older ones stay on the Record. A tenant with no login is ErrNoPool, an empty argument is ErrBadKey and a nil reader is ErrConfig; a read that fails is an error that carries no
// driver text. Both reads run in one read-only transaction.
func (d *Decisions) AllowOf(ctx context.Context, tenantID, correlationID string) (orchestrator.Decision, error) {
	var none orchestrator.Decision
	if d == nil {
		return none, ErrConfig
	}
	if tenantID == "" || correlationID == "" {
		return none, ErrBadKey
	}
	pool, err := d.pools.For(tenantID)
	if err != nil {
		return none, err
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return none, errAllowRead
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // a read-only transaction has nothing to lose
	rows, err := tx.Query(ctx, allowSQL, tenantID, correlationID)
	if err != nil {
		return none, errAllowRead
	}
	var found []orchestrator.Decision
	for rows.Next() {
		var id, intentID string
		var version *int32
		if err := rows.Scan(&id, &intentID, &version); err != nil {
			rows.Close()
			return none, errAllowRead
		}
		dec := orchestrator.Decision{ID: id, Outcome: contracts.EnumsV1DecisionOutcomeALLOW}
		if version != nil && intentID != "" {
			dec.IntentRef = fmt.Sprintf("%s@%d", intentID, *version)
		}
		found = append(found, dec)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return none, errAllowRead
	}
	switch {
	case len(found) == 0:
		return none, trustport.ErrNoRecord
	case len(found) > 1: // two admitted versions with one hash: the reference is ambiguous, so nothing binds
		return none, fmt.Errorf("%w: the intent of the decision names more than one admitted version", trustport.ErrBadAnswer)
	}
	dec := found[0]
	grows, err := tx.Query(ctx, allowGrantsSQL, tenantID, dec.ID)
	if err != nil {
		return none, errAllowRead
	}
	defer grows.Close()
	for grows.Next() {
		var g string
		if err := grows.Scan(&g); err != nil {
			return none, errAllowRead
		}
		dec.GrantIDs = append(dec.GrantIDs, g)
	}
	if err := grows.Err(); err != nil {
		return none, errAllowRead
	}
	return dec, nil
}
