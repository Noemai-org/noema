package trustport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// The production RouteScopes (TRU-12; A625, interim, relayed, not yet committed; REQ-TRU-010, REQ-TRU-009, REQ-SEC-004; INV-12, INV-03, BD-05). It is Trust-side: it runs under the
// tenant's own Trust login (trust_rw), reads sealed snapshots and admitted intents, and never reads the investigation. The session is the one the orchestrator read with its own login and
// handed over on the request; this reader only checks it names the tenant, a principal and a role. Its tests keep it from importing the investigations package or naming that table.

// ErrNoRouteScope: the correlation has no sealed snapshot of the tenant, or its newest one was sealed before the latest admitted intent (A668), so there is no scope and no grant (A625).
var ErrNoRouteScope = errors.New("trustport: the correlation has no sealed snapshot to decide the route on")

const (
	// selectNewestSnapshot takes the newest snapshot of the correlation by its seal time, and the greater snapshot id when two were sealed at the same instant (A625). The table holds one
	// time, sealed_at, written with the row; there is no other clock to order by.
	selectNewestSnapshot = `SELECT snapshot_id, sealed_at FROM context_snapshot WHERE tenant_id = $1 AND correlation_id = $2 ORDER BY sealed_at DESC, snapshot_id DESC LIMIT 1`
)

// RouteScopeReader implements RouteScopes over the tenant's own Trust login. The database, not this code, binds every read to the tenant (row-level security through the login's own
// mapping); the queries also name the tenant.
type RouteScopeReader struct {
	logins  SnapshotLogins
	timeout time.Duration
}

var _ RouteScopes = RouteScopeReader{}

// NewRouteScopeReader builds the reader. The timeout comes from configuration (NOEMA_TRUST_DB_TIMEOUT_MS); a missing login source or a timeout below one is ErrConfig (AGENT-16).
func NewRouteScopeReader(logins SnapshotLogins, timeout time.Duration) (RouteScopeReader, error) {
	if logins == nil || timeout <= 0 {
		return RouteScopeReader{}, ErrConfig
	}
	return RouteScopeReader{logins: logins, timeout: timeout}, nil
}

// ScopeFor returns the scope of a route: the handed session, the correlation's latest admitted intent and its newest sealed snapshot, read in one read-only repeatable-read
// transaction of the tenant's Trust login. A session that is not the tenant's or names no principal or role is ErrSession; no snapshot, or a newest snapshot sealed before the latest admitted intent (A668), is ErrNoRouteScope; no admitted intent is
// ErrIntentRef; in each case the scope is the zero value and no grant follows. A snapshot whose hash does not bind is refused by Trust's own basis binding, not here.
func (r RouteScopeReader) ScopeFor(ctx context.Context, tenantID, correlationID string, session intake.Session) (RouteScope, error) {
	if r.logins == nil || r.timeout <= 0 {
		return RouteScope{}, ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok {
		return RouteScope{}, ErrEvidenceNoDeadline
	}
	if tenantID == "" || correlationID == "" || session.TenantID != tenantID || session.PrincipalID == "" || session.Role == "" {
		return RouteScope{}, ErrSession
	}
	src, err := r.logins.TxFor(tenantID)
	if err != nil {
		return RouteScope{}, wrapped(ErrSnapshotStore, err)
	}
	if src == nil {
		return RouteScope{}, wrapped(ErrSnapshotStore, nil)
	}
	qctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := src.BeginTx(qctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return RouteScope{}, wrapped(ErrSnapshotStore, err)
	}
	defer func() {
		rctx, stop := context.WithTimeout(context.WithoutCancel(ctx), r.timeout)
		defer stop()
		_ = tx.Rollback(rctx) // a read-only transaction has nothing to lose
	}()
	var snapshotID string
	var sealed time.Time
	switch err := tx.QueryRow(qctx, selectNewestSnapshot, tenantID, correlationID).Scan(&snapshotID, &sealed); {
	case errors.Is(err, pgx.ErrNoRows):
		return RouteScope{}, ErrNoRouteScope
	case err != nil:
		return RouteScope{}, wrapped(ErrSnapshotStore, err)
	}
	var intentID string
	var version int
	var node string
	var admitted time.Time
	switch err := tx.QueryRow(qctx, selectIntent, tenantID, correlationID).Scan(&intentID, &version, &node, &admitted); {
	case errors.Is(err, pgx.ErrNoRows):
		return RouteScope{}, fmt.Errorf("%w: the correlation has no admitted intent", ErrIntentRef)
	case err != nil:
		return RouteScope{}, wrapped(ErrSnapshotStore, err)
	}
	// A668 (interim, relayed; INV-03, INV-17, INV-21): the newest snapshot must not predate the admitted intent. sealed_at is the POC's ordering authority (one core process, one clock
	// interface; DEBT-58 holds the database-defaulted column needed before any multi-sealer or Phase 2 deployment). Strictly older is refused; an equal instant is allowed.
	if sealed.Before(admitted) {
		return RouteScope{}, ErrNoRouteScope
	}
	return RouteScope{Session: session, IntentRef: fmt.Sprintf("%s@%d", intentID, version), EvidenceSnapshotID: snapshotID}, nil
}
