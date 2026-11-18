package trustpg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

// The zero records, returned with an error. A composite literal of DecisionRecord is the construction the lint rule reserves for trust/decide; this one is a read-back type.
var (
	none    trustport.DecisionRecord
	noGrant trustport.GrantRecord
)

// Records implements trustport.Records: Trust's own decision and grant records, read as the tenant's trust_rw login under forced row-level security. The tenant is also in every
// WHERE, so a wrong pool and a wrong argument each stay harmless alone. A record that is absent or another tenant's is trustport.ErrNoRecord, the same answer for both.
type Records struct {
	pools   Pools
	timeout time.Duration
}

var _ trustport.Records = (*Records)(nil)

// NewRecords builds the reader. No pool or a timeout below 1ns is ErrConfig.
func NewRecords(p Pools, timeout time.Duration) (*Records, error) {
	if err := p.usable(timeout); err != nil {
		return nil, err
	}
	return &Records{pools: p, timeout: timeout}, nil
}

// decisionSQL reads the decision and finds its intent reference from the admitted intent whose id and hash the decision names: the record carries the intent ID and hash and no
// version, and the hash belongs to exactly one version. No such intent leaves the version NULL, which is an empty reference that no Binding can match.
const decisionSQL = `SELECT d.decision_id, d.outcome, COALESCE(d.json ->> 'intent_id', ''), a.version
FROM decision_record d
LEFT JOIN admitted_intent a ON a.tenant_id = d.tenant_id AND a.intent_id = d.json ->> 'intent_id' AND a.intent_hash = d.intent_hash
WHERE d.tenant_id = $1 AND d.decision_id = $2`

// Decision reads one decision as the tenant.
func (r *Records) Decision(ctx context.Context, tenantID, decisionID string) (trustport.DecisionRecord, error) {
	var d trustport.DecisionRecord
	pool, ctx, cancel, err := r.begin(ctx, tenantID, decisionID)
	if err != nil {
		return d, err
	}
	defer cancel()
	rows, err := pool.Query(ctx, decisionSQL, tenantID, decisionID)
	if err != nil {
		return d, errors.New("trustpg: the decision read failed") // the driver's error can name the server and the login
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var intentID string
		var version *int32
		if err := rows.Scan(&d.ID, &d.Outcome, &intentID, &version); err != nil {
			return none, errors.New("trustpg: the decision read failed")
		}
		found++
		d.IntentRef = ""
		if version != nil && intentID != "" {
			d.IntentRef = fmt.Sprintf("%s@%d", intentID, *version)
		}
	}
	if err := rows.Err(); err != nil {
		return none, errors.New("trustpg: the decision read failed")
	}
	switch {
	case found == 0:
		return none, trustport.ErrNoRecord
	case found > 1: // two admitted versions with one hash: the reference is ambiguous, so nothing binds
		return none, fmt.Errorf("%w: the intent of the decision names more than one admitted version", trustport.ErrBadAnswer)
	}
	return d, nil
}

const grantSQL = `SELECT grant_id, decision_id, COALESCE(json ->> 'correlation_id', ''), COALESCE(json ->> 'principal_id', '') FROM decision_grant WHERE tenant_id = $1 AND grant_id = $2`

// Grant reads one grant as the tenant.
func (r *Records) Grant(ctx context.Context, tenantID, grantID string) (trustport.GrantRecord, error) {
	var g trustport.GrantRecord
	pool, ctx, cancel, err := r.begin(ctx, tenantID, grantID)
	if err != nil {
		return g, err
	}
	defer cancel()
	err = pool.QueryRow(ctx, grantSQL, tenantID, grantID).Scan(&g.ID, &g.DecisionID, &g.CorrelationID, &g.PrincipalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return noGrant, trustport.ErrNoRecord
	}
	if err != nil {
		return noGrant, errors.New("trustpg: the grant read failed")
	}
	return g, nil
}

func (r *Records) begin(ctx context.Context, tenantID, id string) (querier, context.Context, context.CancelFunc, error) {
	if r == nil || tenantID == "" || id == "" {
		return nil, nil, nil, ErrConfig
	}
	pool, err := r.pools.For(tenantID)
	if err != nil {
		return nil, nil, nil, err
	}
	c, cancel := context.WithTimeout(ctx, r.timeout)
	return pool, c, cancel, nil
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
