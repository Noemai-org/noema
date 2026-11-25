package trustpg

import (
	"context"
	"errors"
	"fmt"
	"time"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

// Dispositions implements trustport.Dispositions over the Decision Record (A539; REQ-TRU-010, INV-12, INV-30): the ESCALATE disposition decision of a correlation, read as the tenant's
// own trust_rw login under forced row-level security. Read-only.
type Dispositions struct {
	pools   Pools
	timeout time.Duration
}

var _ trustport.Dispositions = (*Dispositions)(nil)

// NewDispositions builds the reader. No pool or a timeout below 1ns is ErrConfig.
func NewDispositions(p Pools, timeout time.Duration) (*Dispositions, error) {
	if err := p.usable(timeout); err != nil {
		return nil, err
	}
	return &Dispositions{pools: p, timeout: timeout}, nil
}

// dispositionSQL finds the ESCALATE decisions of the correlation. The tenant is in the WHERE and, under forced row-level security, in the login: a wrong pool and a wrong argument each
// stay harmless alone. At most two rows are read: one is the disposition, two are an ambiguity.
const dispositionSQL = `SELECT decision_id FROM decision_record WHERE tenant_id = $1 AND outcome = 'ESCALATE' AND json ->> 'correlation_id' = $2 ORDER BY created_at, decision_id LIMIT 2`

// DispositionFor returns the decision ID of the ESCALATE decision the Record holds for the correlation, as the tenant. None (absent, or another tenant's: the same answer) is
// trustport.ErrNoDisposition. Two are trustport.ErrBadAnswer with no id: none is guessed. A tenant with no login is ErrNoPool, an empty argument is ErrBadKey and a nil reader is
// ErrConfig; a read that fails is an error that carries no driver text.
func (d *Dispositions) DispositionFor(ctx context.Context, tenantID, correlationID string) (string, error) {
	if d == nil {
		return "", ErrConfig
	}
	if tenantID == "" || correlationID == "" {
		return "", ErrBadKey
	}
	pool, err := d.pools.For(tenantID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	rows, err := pool.Query(ctx, dispositionSQL, tenantID, correlationID)
	if err != nil {
		return "", errors.New("trustpg: the disposition read failed") // the driver's error can name the server and the login
	}
	defer rows.Close()
	var found []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", errors.New("trustpg: the disposition read failed")
		}
		found = append(found, id)
	}
	if err := rows.Err(); err != nil {
		return "", errors.New("trustpg: the disposition read failed")
	}
	switch len(found) {
	case 0:
		return "", trustport.ErrNoDisposition
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("%w: the correlation has more than one ESCALATE decision", trustport.ErrBadAnswer)
}
