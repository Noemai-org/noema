// Package recoverypg is the Postgres reader of the recovery pass (A696; STM-05; REQ-STM-001, REQ-RCV-003, REQ-SEC-004; INV-03, INV-12; A476, A477, A490).
//
// Reader implements recovery.Reader: it lists a tenant's idle investigations in the configured states, oldest first, as the tenant's own orchestrator login under forced row-level
// security, in a read-only transaction, and gives each the ALLOW decision Trust recorded for its correlation (through the Decisions port, Trust's own read). Idleness is measured by the
// DATABASE's clock; the idle age and the batch are the caller's arguments (recovery.Config: no default in code, AGENT-16). It writes nothing, dispatches nothing and decides nothing: the
// activity resumes from the Record, and what it cannot resume stops for a person.
//
// May import: the standard library, pgx, orchestrator, orchestrator/recovery and the trust port's error values. Must never: write a row, import the trust, execution or outcome internals,
// or fall back to another tenant's login.
package recoverypg

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
)

var (
	// ErrConfig is returned for a reader built without what it needs, or asked a question that is not complete.
	ErrConfig = errors.New("recoverypg: the reader is not configured")
	// ErrNoPool is returned for a tenant that has no login. It never falls back to another tenant's login (INV-12), and it is not "no investigation": the caller could not tell.
	ErrNoPool = errors.New("recoverypg: no login is configured for this tenant")

	errRead = errors.New("recoverypg: the idle investigations could not be read") // the driver's error can name the server and the login
)

// Decisions is Trust's side of the read: the ALLOW decision of a correlation as the tenant, or an error that is trustport.ErrNoRecord when there is none. It is read-only.
type Decisions interface {
	AllowOf(ctx context.Context, tenantID, correlationID string) (orchestrator.Decision, error)
}

// Reader implements recovery.Reader. Build it with New.
type Reader struct {
	pools     map[string]*pgxpool.Pool
	decisions Decisions
	states    []string
	timeout   time.Duration
}

var _ recovery.Reader = (*Reader)(nil)

// New builds the reader whole or not at all (ErrConfig). pools holds one orchestrator login per tenant. states are the investigation states a pass takes up: each must exist in the
// transition table and must not be terminal (a terminal investigation has nothing to resume), and none may repeat. The timeout bounds one read and has no default.
func New(pools map[string]*pgxpool.Pool, decisions Decisions, states []orchestrator.State, timeout time.Duration) (*Reader, error) {
	if len(pools) == 0 || decisions == nil || len(states) == 0 || timeout <= 0 {
		return nil, ErrConfig
	}
	copied := make(map[string]*pgxpool.Pool, len(pools))
	for t, p := range pools {
		if t == "" || p == nil {
			return nil, ErrConfig
		}
		copied[t] = p
	}
	table, err := orchestrator.LoadTable()
	if err != nil {
		return nil, ErrConfig
	}
	known := map[orchestrator.State]bool{}
	for _, s := range table.States() {
		known[s] = true
	}
	seen := map[orchestrator.State]bool{}
	names := make([]string, 0, len(states))
	for _, s := range states {
		if !known[s] || seen[s] || len(table.Exits(s)) == 0 {
			return nil, ErrConfig
		}
		seen[s] = true
		names = append(names, string(s))
	}
	return &Reader{pools: copied, decisions: decisions, states: names, timeout: timeout}, nil
}

var tenantRe = regexp.MustCompile(`\Aten_[0-9A-Za-z]{2,32}\z`)

// idleSQL takes the tenant's rows in the configured states whose last move is older than the idle age on the database's own clock, oldest first, at most the batch. The tenant is in the
// WHERE and, under forced row-level security, in the login: a wrong pool and a wrong argument each stay harmless alone.
const idleSQL = `SELECT tenant_id, investigation_id, state, state_version, input_received_ref, correlation_id, principal_ref
FROM investigation
WHERE tenant_id = $1 AND state = ANY ($2) AND updated_at < now() - ($3::double precision * interval '1 millisecond')
ORDER BY updated_at, investigation_id LIMIT $4`

// Idle lists the tenant's idle investigations (recovery.Reader). A question that is not complete (no deadline, a tenant outside its pattern, an idle age or a limit below one) is
// refused before any query; a tenant with no login is ErrNoPool. A read that cannot be answered, on either side, is an error and never an empty list. A correlation with no recorded
// ALLOW gives a candidate with no decision, which the activity sends to a person.
func (r *Reader) Idle(ctx context.Context, tenantID string, idleMS int64, limit int) ([]recovery.Candidate, error) {
	if r == nil || r.decisions == nil {
		return nil, ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok || !tenantRe.MatchString(tenantID) || idleMS < 1 || limit < 1 {
		return nil, fmt.Errorf("%w: the question is not complete", ErrConfig)
	}
	pool, ok := r.pools[tenantID]
	if !ok {
		return nil, ErrNoPool
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, errRead
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // a read-only transaction has nothing to lose
	rows, err := tx.Query(ctx, idleSQL, tenantID, r.states, float64(idleMS), limit)
	if err != nil {
		return nil, errRead
	}
	var out []recovery.Candidate
	for rows.Next() {
		var c recovery.Candidate
		var state string
		if err := rows.Scan(&c.TenantID, &c.Investigation.ID, &state, &c.Investigation.Version, &c.InputReceivedRef, &c.CorrelationID, &c.PrincipalRef); err != nil {
			rows.Close()
			return nil, errRead
		}
		c.Investigation.State = orchestrator.State(state)
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errRead
	}
	for i := range out {
		d, err := r.decisions.AllowOf(ctx, tenantID, out[i].CorrelationID)
		switch {
		case err == nil:
			out[i].Decision = d
		case errors.Is(err, trustport.ErrNoRecord):
			out[i].Decision = orchestrator.Decision{}
		default:
			return nil, fmt.Errorf("recoverypg: the recorded decision could not be read: %w", err)
		}
	}
	return out, nil
}
