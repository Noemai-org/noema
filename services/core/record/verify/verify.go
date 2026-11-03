package verify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const errPrefix = "verify: "

// rollbackTimeout bounds the rollback of a snapshot after the run's own context has ended.
const rollbackTimeout = 5 * time.Second

// BreakHeadMismatch is the break found when the stored chain head is not the newest event: events were dropped from the
// tail, the head row was removed, or the head was moved. chain.Verify cannot see this, because a shorter chain still
// verifies. It does not close the case of an attacker who rewrites the head as well; signed exported checkpoints do
// (REC-07).
const BreakHeadMismatch chain.BreakKind = "HEAD_MISMATCH"

// Status is the result of verifying one tenant.
type Status string

// The three results. ERROR means the verifier could not read: that is neither a pass nor a break.
const (
	StatusOK    Status = "OK"
	StatusBreak Status = "BREAK"
	StatusError Status = "ERROR"
)

// Finding is one chain break, as the alert and the incident carry it. It holds identifiers and a fixed description, never
// event contents.
type Finding struct {
	TenantID string
	Seq      int64
	Kind     chain.BreakKind
	Detail   string
}

// Alerts raises the security alert and opens the S1 incident of REQ-AUD-005. Its failure is returned by the job, never
// swallowed.
type Alerts interface {
	ChainBreak(ctx context.Context, f Finding) error
}

// Metrics counts breaks: noema_record_chain_break_total{kind} (PERD §44.1; the label is an enum).
type Metrics interface {
	ChainBreak(kind string)
}

// DB is what the job needs of the database: a read-only snapshot per tenant, the tenant list, and the kill switch.
type DB interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Verifier verifies the Record. It reads and, on a break, sets the tenant kill switch; it never repairs a chain.
type Verifier struct {
	DB      DB
	Alerts  Alerts
	Metrics Metrics
	Clock   clock.Clock
}

// TenantResult is the outcome for one tenant.
type TenantResult struct {
	TenantID string
	Checked  int64    // events whose hashes were recomputed
	Status   Status   // OK, BREAK or ERROR
	Finding  *Finding // set when Status is BREAK
	Err      error    // set when Status is ERROR, or when the alert or the kill switch failed
}

// Report is the outcome of one run.
type Report struct {
	At      time.Time
	Results []TenantResult
}

// Clean reports whether every tenant verified and there was at least one.
func (r Report) Clean() bool {
	if len(r.Results) == 0 {
		return false
	}
	for _, t := range r.Results {
		if t.Status != StatusOK {
			return false
		}
	}
	return true
}

func needDeadline(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New(errPrefix + "the context has no deadline")
	}
	return nil
}

// Run verifies every tenant that has a tenant row, a chain head or an event, in tenant order. A break in one tenant does
// not stop the others. The error is non-nil if any tenant could not be read, or any alert or kill switch failed; the
// report is returned either way.
func (v *Verifier) Run(ctx context.Context) (Report, error) {
	rep := Report{At: v.Clock.Now()}
	if err := needDeadline(ctx); err != nil {
		return rep, err
	}
	tenants, err := v.tenants(ctx)
	if err != nil {
		return rep, err
	}
	var errs []error
	for _, id := range tenants {
		res, err := v.verifyTenant(ctx, id)
		rep.Results = append(rep.Results, res)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return rep, errors.Join(errs...)
}

// RunTenant is the on-demand form for one tenant. The ID must have the shape of a tenant ID and name a tenant that is
// known to the database.
func (v *Verifier) RunTenant(ctx context.Context, tenantID string) (TenantResult, error) {
	if err := needDeadline(ctx); err != nil {
		return TenantResult{TenantID: tenantID, Status: StatusError, Err: err}, err
	}
	if !chain.ValidTenantID(tenantID) {
		err := fmt.Errorf("%snot a tenant ID", errPrefix)
		return TenantResult{TenantID: tenantID, Status: StatusError, Err: err}, err
	}
	known, err := v.tenants(ctx)
	if err != nil {
		return TenantResult{TenantID: tenantID, Status: StatusError, Err: err}, err
	}
	for _, id := range known {
		if id == tenantID {
			return v.verifyTenant(ctx, tenantID)
		}
	}
	err = fmt.Errorf("%sunknown tenant", errPrefix)
	return TenantResult{TenantID: tenantID, Status: StatusError, Err: err}, err
}

func (v *Verifier) tenants(ctx context.Context) ([]string, error) {
	rows, err := v.DB.Query(ctx, `SELECT tenant_id FROM tenant UNION SELECT tenant_id FROM record_chain_head UNION
		SELECT tenant_id FROM record_event ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("%slist tenants: %w", errPrefix, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("%slist tenants: %w", errPrefix, err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%slist tenants: %w", errPrefix, err)
	}
	return out, nil
}

// verifyTenant checks one tenant's chain and head in one read-only snapshot, so an append that lands meanwhile cannot look
// like a break. On a break it counts it, stops the tenant's writes, then raises the alert: in that order, so an alert that
// cannot be delivered still leaves the tenant stopped.
func (v *Verifier) verifyTenant(ctx context.Context, tenantID string) (TenantResult, error) {
	res := TenantResult{TenantID: tenantID}
	finding, checked, err := v.check(ctx, tenantID)
	res.Checked = checked
	if err != nil {
		res.Status, res.Err = StatusError, err
		return res, err
	}
	if finding == nil {
		res.Status = StatusOK
		return res, nil
	}
	res.Status, res.Finding = StatusBreak, finding
	if v.Metrics != nil {
		v.Metrics.ChainBreak(string(finding.Kind))
	}
	var errs []error
	if err := v.halt(ctx, tenantID); err != nil {
		errs = append(errs, err)
	}
	if err := v.Alerts.ChainBreak(ctx, *finding); err != nil {
		errs = append(errs, fmt.Errorf("%sraise the alert for %s: %w", errPrefix, tenantID, err))
	}
	res.Err = errors.Join(errs...)
	return res, res.Err
}

func (v *Verifier) check(ctx context.Context, tenantID string) (*Finding, int64, error) {
	tx, err := v.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("%sbegin: %w", errPrefix, err)
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rctx)
	}()
	checked, err := chain.Verify(ctx, tx, tenantID)
	var be *chain.BreakError
	if errors.As(err, &be) {
		return &Finding{TenantID: be.TenantID, Seq: be.Seq, Kind: be.Kind, Detail: be.Detail}, checked, nil
	}
	if err != nil {
		return nil, checked, err
	}
	f, err := checkHead(ctx, tx, tenantID, checked)
	return f, checked, err
}

// checkHead compares the stored chain head with the newest event. With no events the head must be at the genesis.
func checkHead(ctx context.Context, tx pgx.Tx, tenantID string, events int64) (*Finding, error) {
	var headSeq int64
	var headHash string
	err := tx.QueryRow(ctx, `SELECT seq, hash FROM record_chain_head WHERE tenant_id = $1`, tenantID).Scan(&headSeq, &headHash)
	mismatch := func(detail string) *Finding {
		return &Finding{TenantID: tenantID, Seq: headSeq, Kind: BreakHeadMismatch, Detail: detail}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return mismatch("the chain head row is missing"), nil
	}
	if err != nil {
		return nil, fmt.Errorf("%sread head: %w", errPrefix, err)
	}
	wantSeq, wantHash := events, chain.GenesisHash(tenantID)
	if events > 0 {
		if err := tx.QueryRow(ctx, `SELECT hash FROM record_event WHERE tenant_id = $1 AND seq = $2`, tenantID, events).Scan(&wantHash); err != nil {
			return nil, fmt.Errorf("%sread newest event: %w", errPrefix, err)
		}
	}
	if headSeq != wantSeq || headHash != wantHash {
		return mismatch("the chain head is not the newest event"), nil
	}
	return nil, nil
}

// halt turns the tenant's kill switch on (E6 reads it before every write). A tenant with no row cannot be stopped, and
// that is an error: the alert still goes out.
func (v *Verifier) halt(ctx context.Context, tenantID string) error {
	tag, err := v.DB.Exec(ctx, `UPDATE tenant SET kill_switch = true WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return fmt.Errorf("%sstop writes for %s: %w", errPrefix, tenantID, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%sstop writes for %s: no such tenant row", errPrefix, tenantID)
	}
	return nil
}
