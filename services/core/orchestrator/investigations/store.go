// Package investigations stores the investigation projection of PERD 28.2 (STM-01 slice 2): one row per investigation, unique per recorded input, moved by a
// compare-and-swap on its version in the same transaction that appends the Record event INVESTIGATION_STATE_CHANGED (the Record comes first, PERD 32.4, EC-E16).
//
// May import: the standard library, pgx, internal packages, orchestrator, record/chain.
//
// Must never: decide a state (the machine does), take a tenant from anything but the login (the database binds it), or write a row without its Record event.
package investigations

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	rollbackTimeout = 5 * time.Second // the bound of a rollback after a failure, on its own deadline: the request's may be spent
	errPrefix       = "investigations: "
	emitter         = "core-workflows" // the orchestrator's Record identity (migration 0004)
	eventType       = "INVESTIGATION_STATE_CHANGED"
)

var (
	// ErrRequest marks a request that cannot be honoured; nothing was written.
	ErrRequest = errors.New(errPrefix + "the request is not valid")
	// ErrNoRecordedInput marks an input the Record does not confirm as an INPUT_RECEIVED of this tenant, principal and correlation: no investigation is made for it.
	ErrNoRecordedInput = errors.New(errPrefix + "the Record does not confirm this input")
)

// Inputs confirms, against the Record, that the INPUT_RECEIVED event a Start names exists and belongs to this session and correlation (A161, A169). A nil return is the
// confirmation; any error means the input is not confirmed.
type Inputs interface {
	Confirm(ctx context.Context, sess orchestrator.Session, ref, correlationID string) error
}

// Recorded is what the Record holds for one event, as a Lookuper returns it.
type Recorded struct{ EventType, TenantID, PrincipalID, CorrelationID string }

// Lookuper reads one event of a tenant's Record by ID (record/admission.Reader, through a thin adapter in the composition root).
type Lookuper interface {
	Lookup(ctx context.Context, tenantID, eventID string) (Recorded, error)
}

// ConfirmWith turns a Lookuper into Inputs: the event must exist and be an INPUT_RECEIVED of the session's tenant and principal and the request's correlation.
func ConfirmWith(l Lookuper) Inputs { return lookupInputs{l} }

type lookupInputs struct{ l Lookuper }

func (c lookupInputs) Confirm(ctx context.Context, sess orchestrator.Session, ref, correlationID string) error {
	r, err := c.l.Lookup(ctx, sess.TenantID, ref)
	if err != nil {
		return fmt.Errorf("%w: lookup: %w", ErrNoRecordedInput, err)
	}
	if r.EventType != "INPUT_RECEIVED" || r.TenantID != sess.TenantID || r.PrincipalID == "" || r.PrincipalID != sess.PrincipalID || r.CorrelationID != correlationID {
		return ErrNoRecordedInput
	}
	return nil
}

var (
	tenantRe        = regexp.MustCompile(`\Aten_[0-9A-Za-z]{2,32}\z`)
	investigationRe = regexp.MustCompile(`\Ainv_[0-9A-Za-z]{2,32}\z`)
	correlationRe   = regexp.MustCompile(`\Acorr_[0-9A-Za-z]{2,32}\z`)
	recordedRe      = regexp.MustCompile(`\Arecev_[0-9A-Za-z]{2,32}\z`)
)

// Pool starts the transactions of the orchestrator's login. Its connections authenticate as the tenant's own orchestrator login, so the database, not this
// package, decides which tenant's rows they can see and write (row-level security with FORCE, migration 0025).
type Pool interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Store is the investigation store. Appender writes the Record event in the store's transaction.
type Store struct {
	Pool     Pool
	Appender *chain.Appender
	Inputs   Inputs // required: Start makes an investigation only for an input the Record confirms
}

// StartRequest names a new investigation for one recorded INPUT_RECEIVED (A161).
type StartRequest struct {
	InvestigationID  string // proposed; the row of an earlier Start for the same input keeps its own
	CorrelationID    string
	InputReceivedRef string // the Record event ID the input gateway wrote
}

func needDeadline(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New(errPrefix + "the context needs a deadline")
	}
	return nil
}

func (s *Store) ready() error {
	if s == nil || s.Pool == nil || s.Appender == nil || s.Inputs == nil {
		return fmt.Errorf("%w: the store is not configured", ErrRequest)
	}
	return nil
}

func checkSession(sess orchestrator.Session) error {
	if !tenantRe.MatchString(sess.TenantID) || sess.PrincipalID == "" {
		return fmt.Errorf("%w: the session has no valid tenant and principal", ErrRequest)
	}
	return nil
}

// Start makes the investigation of a recorded input, or finds the one an earlier Start made (idempotent per tenant and input, noema#477). created is true only for
// the call that made the row. The returned Investigation carries the state and version the row has now; a caller that finds a version above 0 must not run it again.
func (s *Store) Start(ctx context.Context, sess orchestrator.Session, r StartRequest) (orchestrator.Investigation, bool, error) {
	if err := needDeadline(ctx); err != nil {
		return orchestrator.Investigation{}, false, err
	}
	if err := s.ready(); err != nil {
		return orchestrator.Investigation{}, false, err
	}
	if err := checkSession(sess); err != nil {
		return orchestrator.Investigation{}, false, err
	}
	if !investigationRe.MatchString(r.InvestigationID) || !correlationRe.MatchString(r.CorrelationID) || !recordedRe.MatchString(r.InputReceivedRef) {
		return orchestrator.Investigation{}, false, fmt.Errorf("%w: an investigation, correlation or recorded-input reference is outside its pattern", ErrRequest)
	}
	if err := s.Inputs.Confirm(ctx, sess, r.InputReceivedRef, r.CorrelationID); err != nil {
		if errors.Is(err, ErrNoRecordedInput) {
			return orchestrator.Investigation{}, false, err
		}
		return orchestrator.Investigation{}, false, fmt.Errorf("%w: %w", ErrNoRecordedInput, err)
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return orchestrator.Investigation{}, false, fmt.Errorf("%sbegin: %w", errPrefix, err)
	}
	defer rollback(ctx, tx)
	var inv orchestrator.Investigation
	var state, corr string
	err = tx.QueryRow(ctx, `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version)
		VALUES ($1, $2, $3, $4, 'RECEIVED', 0) ON CONFLICT ON CONSTRAINT investigation_one_per_input DO NOTHING
		RETURNING investigation_id, state, state_version, correlation_id`, sess.TenantID, r.InvestigationID, r.InputReceivedRef, r.CorrelationID).Scan(&inv.ID, &state, &inv.Version, &corr)
	created := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT investigation_id, state, state_version, correlation_id FROM investigation WHERE tenant_id = $1 AND input_received_ref = $2`,
			sess.TenantID, r.InputReceivedRef).Scan(&inv.ID, &state, &inv.Version, &corr)
	}
	if err != nil {
		return orchestrator.Investigation{}, false, fmt.Errorf("%sstart: %w", errPrefix, err)
	}
	if corr != r.CorrelationID {
		return orchestrator.Investigation{}, false, fmt.Errorf("%w: the recorded input already belongs to another correlation", ErrRequest)
	}
	if err := tx.Commit(ctx); err != nil {
		return orchestrator.Investigation{}, false, fmt.Errorf("%scommit: %w", errPrefix, err)
	}
	inv.State = orchestrator.State(state)
	return inv, created, nil
}

// AppendChange is the orchestrator.Recorder: in one transaction it locks the investigation's row, refuses (ErrStaleVersion) a change made against another state or
// version than the row's own, appends INVESTIGATION_STATE_CHANGED as core-workflows, and only then moves the row, comparing and swapping on the version the caller
// read. If any step fails the transaction rolls back and neither the event nor the move remains.
func (s *Store) AppendChange(ctx context.Context, sess orchestrator.Session, ch orchestrator.Change) error {
	if err := needDeadline(ctx); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if err := checkSession(sess); err != nil {
		return err
	}
	if !investigationRe.MatchString(ch.ID) || ch.FromVersion < 0 || ch.ToVersion != ch.FromVersion+1 || ch.From == "" || ch.To == "" {
		return fmt.Errorf("%w: a change names an investigation, two states and the next version", ErrRequest)
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%sbegin: %w", errPrefix, err)
	}
	defer rollback(ctx, tx)
	var corr, state string
	var version int64
	err = tx.QueryRow(ctx, `SELECT correlation_id, state, state_version FROM investigation WHERE tenant_id = $1 AND investigation_id = $2 FOR UPDATE`,
		sess.TenantID, ch.ID).Scan(&corr, &state, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: no such investigation for this tenant", ErrRequest)
	}
	if err != nil {
		return fmt.Errorf("%sread: %w", errPrefix, err)
	}
	if version != ch.FromVersion || state != string(ch.From) {
		return fmt.Errorf("%w: the row is %s at version %d", orchestrator.ErrStaleVersion, state, version)
	}
	if _, err := s.Appender.Append(ctx, tx, chain.Event{TenantID: sess.TenantID, CorrelationID: corr, WrittenBy: emitter, Actor: emitter, EventType: eventType, ObjectRef: ch.ID,
		Payload: map[string]any{"investigation_id": ch.ID, "from": string(ch.From), "to": string(ch.To), "from_version": ch.FromVersion, "to_version": ch.ToVersion}}); err != nil {
		return fmt.Errorf("%sappend %s: %w", errPrefix, eventType, err)
	}
	tag, err := tx.Exec(ctx, `UPDATE investigation SET state = $1, state_version = $2, updated_at = now() WHERE tenant_id = $3 AND investigation_id = $4 AND state_version = $5`,
		string(ch.To), ch.ToVersion, sess.TenantID, ch.ID, ch.FromVersion)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23000" {
		return fmt.Errorf("%w: the database refused the move: %s", orchestrator.ErrStaleVersion, pe.Message)
	}
	if err != nil {
		return fmt.Errorf("%smove: %w", errPrefix, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: the row moved first", orchestrator.ErrStaleVersion)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%scommit: %w", errPrefix, err)
	}
	return nil
}

// rollback ends a transaction that did not commit. A rollback after a commit is a no-op, so its error is not actionable.
func rollback(ctx context.Context, tx pgx.Tx) {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(c)
}
