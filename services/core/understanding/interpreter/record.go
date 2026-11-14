package interpreter

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/understanding"
)

const (
	emitter        = "core-workflows" // what the orchestrator's login is bound to write (migration 0004), as for the admitted-intent store
	eventRejected  = "INTENT_REJECTED_AT_GATE"
	eventSecurity  = "SECURITY_EVENT"
	kindOverride   = "PRINCIPAL_OVERRIDE_ATTEMPT"
	objectRefStart = "gat_"
)

var (
	reasonRe   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	overridden = map[string]bool{"tenant_id": true, "principal_id": true, "authn_ref": true} // the identity members the gate reports by name
	suffixRe   = regexp.MustCompile(`^corr_([0-9A-Za-z]{2,32})$`)
)

// RecordRecorder appends the Interpreter's two events as the orchestrator's emitter, one transaction per event. Its Pool must log in as the tenant's own orchestrator
// login: the database, not this code, binds the event to the tenant and to that power's emitter (migration 0004).
type RecordRecorder struct {
	Pool     *pgxpool.Pool
	Appender *chain.Appender
	Timeout  time.Duration // from configuration; each append has its own deadline
}

func (r *RecordRecorder) ready() error {
	if r == nil || r.Pool == nil || r.Appender == nil || r.Timeout <= 0 {
		return errors.New(prefix + "the recorder needs a pool, an appender and a positive timeout")
	}
	return nil
}

func (r *RecordRecorder) subject(s Subject) (string, error) {
	m := suffixRe.FindStringSubmatch(s.CorrelationID)
	if !chain.ValidTenantID(s.TenantID) || !actorRe.MatchString(s.Actor) || m == nil {
		return "", errors.New(prefix + "the event's subject is not a tenant, an actor and a correlation id")
	}
	return objectRefStart + m[1], nil
}

// Rejected appends INTENT_REJECTED_AT_GATE with the reasons the gate gave (codes only, sorted as given, never text of the proposal): the shape the reconstruction reads.
func (r *RecordRecorder) Rejected(ctx context.Context, s Subject, reasons []understanding.Reason) error {
	if err := r.ready(); err != nil {
		return err
	}
	ref, err := r.subject(s)
	if err != nil {
		return err
	}
	if len(reasons) == 0 {
		return errors.New(prefix + "a rejection needs at least one reason")
	}
	codes := make([]string, 0, len(reasons))
	for _, c := range reasons {
		if !reasonRe.MatchString(string(c)) {
			return errors.New(prefix + "a reason is not an upper-case code")
		}
		codes = append(codes, string(c))
	}
	return r.append(ctx, chain.Event{TenantID: s.TenantID, CorrelationID: s.CorrelationID, WrittenBy: emitter, Actor: s.Actor, EventType: eventRejected, ObjectRef: ref,
		Payload: map[string]any{"reason_codes": codes}})
}

// OverrideAttempt appends SECURITY_EVENT of kind PRINCIPAL_OVERRIDE_ATTEMPT with the NAMES of the identity members the model service returned and the gate overwrote
// (REQ-INP-002). A name outside the three the gate reports is refused, and a value is never recorded.
func (r *RecordRecorder) OverrideAttempt(ctx context.Context, s Subject, fields []string) error {
	if err := r.ready(); err != nil {
		return err
	}
	ref, err := r.subject(s)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return errors.New(prefix + "an override attempt needs at least one field name")
	}
	for _, f := range fields {
		if !overridden[f] {
			return errors.New(prefix + "a field name is not one the gate reports")
		}
	}
	return r.append(ctx, chain.Event{TenantID: s.TenantID, CorrelationID: s.CorrelationID, WrittenBy: emitter, Actor: s.Actor, EventType: eventSecurity, Kind: kindOverride, ObjectRef: ref,
		Payload: map[string]any{"fields": fields}})
}

func (r *RecordRecorder) append(ctx context.Context, ev chain.Event) error {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%sbegin: %w", prefix, err)
	}
	if _, err := r.Appender.Append(ctx, tx, ev); err != nil {
		rb, stop := context.WithTimeout(context.Background(), r.Timeout)
		defer stop()
		_ = tx.Rollback(rb)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%scommit: %w", prefix, err)
	}
	return nil
}

var _ Recorder = (*RecordRecorder)(nil)
