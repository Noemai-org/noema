package investigations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrAmbiguousInvestigation: more than one investigation of the tenant carries the correlation. The unique keys of the table (the investigation and the recorded input) do not
// make a correlation unique, so a by-correlation read that finds two refuses and names neither (A625, interim, relayed, not yet committed).
var ErrAmbiguousInvestigation = errors.New(errPrefix + "more than one investigation carries this correlation")

// SessionByCorrelation is the orchestrator's by-correlation read of the session a route needs (A625, interim, relayed, not yet committed; TRU-12; REQ-TRU-010, REQ-SEC-004; INV-12): the
// principal, the role and the authn reference the investigation recorded at its start. It runs in a read-only transaction of the orchestrator's own login, so forced row-level
// security binds it to the tenant, and the query names the tenant as well. It says what the row says and decides nothing. A correlation with no investigation of this tenant,
// or only another tenant's, is ErrNoInvestigation: the two are not told apart. Several investigations of one correlation is ErrAmbiguousInvestigation and none is returned. A row
// from before migration 0036 is ErrNoRecordedSession. The read needs the pool only (no Record is written), and the tenant comes from the caller's verified composition, never a request.
func (s *Store) SessionByCorrelation(ctx context.Context, tenantID, correlationID string) (RecordedSession, error) {
	if err := needDeadline(ctx); err != nil {
		return RecordedSession{}, err
	}
	if s == nil || s.Pool == nil {
		return RecordedSession{}, fmt.Errorf("%w: the store is not configured", ErrRequest)
	}
	if !tenantRe.MatchString(tenantID) || !correlationRe.MatchString(correlationID) {
		return RecordedSession{}, fmt.Errorf("%w: a tenant or correlation is outside its pattern", ErrRequest)
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return RecordedSession{}, fmt.Errorf("%sbegin: %w", errPrefix, err)
	}
	defer rollback(ctx, tx)
	rows, err := tx.Query(ctx, `SELECT principal_ref, principal_role, authn_ref FROM investigation WHERE tenant_id = $1 AND correlation_id = $2 ORDER BY investigation_id LIMIT 2`, tenantID, correlationID)
	if err != nil {
		return RecordedSession{}, fmt.Errorf("%sread: %w", errPrefix, err)
	}
	type row struct {
		principal string
		role, ref *string
	}
	found, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		return x, r.Scan(&x.principal, &x.role, &x.ref)
	})
	if err != nil {
		return RecordedSession{}, fmt.Errorf("%sread: %w", errPrefix, err)
	}
	switch len(found) {
	case 0:
		return RecordedSession{}, ErrNoInvestigation
	case 1:
	default:
		return RecordedSession{}, ErrAmbiguousInvestigation
	}
	if f := found[0]; f.role == nil || f.ref == nil || f.principal == "" {
		return RecordedSession{}, ErrNoRecordedSession
	} else {
		return RecordedSession{PrincipalID: f.principal, PrincipalRole: *f.role, AuthnRef: *f.ref}, nil
	}
}
