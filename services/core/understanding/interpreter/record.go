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
	eventResumed   = "INVESTIGATION_RESUMED"
	kindOverride   = "PRINCIPAL_OVERRIDE_ATTEMPT"
	kindCrossScope = "CROSS_SCOPE_ATTEMPT"
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
		rb, stop := context.WithTimeout(context.WithoutCancel(ctx), r.Timeout)
		defer stop()
		_ = tx.Rollback(rb)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%scommit: %w", prefix, err)
	}
	return nil
}

// Resumed appends INVESTIGATION_RESUMED for a follow-up (REQ-INP-004): the investigation it resumed is named by the prior's intent id and version, in the payload and as the object, and nothing of the
// request is written (AGENT-13). An intent id or a version the store could not hold is refused and nothing is appended.
func (r *RecordRecorder) Resumed(ctx context.Context, s Subject, priorIntentID string, priorVersion int) error {
	if err := r.ready(); err != nil {
		return err
	}
	if _, err := r.subject(s); err != nil {
		return err
	}
	if !priorIntentRe.MatchString(priorIntentID) || priorVersion < 1 {
		return errors.New(prefix + "a resume names the prior intent by its id and a version of one or more")
	}
	return r.append(ctx, chain.Event{TenantID: s.TenantID, CorrelationID: s.CorrelationID, WrittenBy: emitter, Actor: s.Actor, EventType: eventResumed, ObjectRef: priorIntentID,
		Payload: map[string]any{"prior_intent_id": priorIntentID, "prior_version": priorVersion}})
}

// Denied appends SECURITY_EVENT of kind CROSS_SCOPE_ATTEMPT for a follow-up that named another principal's investigation (REQ-INP-004; understanding/resume.Denials): for the session's tenant and the
// asking actor, with the chain it asked for as the subject. The payload names the scope only, never a principal or a reference of the other customer (AGENT-13). A missing tenant, actor or chain is
// refused and nothing is appended.
func (r *RecordRecorder) Denied(ctx context.Context, tenantID, actor, correlationID string) error {
	if err := r.ready(); err != nil {
		return err
	}
	s := Subject{TenantID: tenantID, Actor: actor, CorrelationID: correlationID}
	ref, err := r.subject(s)
	if err != nil {
		return err
	}
	return r.append(ctx, chain.Event{TenantID: s.TenantID, CorrelationID: s.CorrelationID, WrittenBy: emitter, Actor: s.Actor, EventType: eventSecurity, Kind: kindCrossScope, ObjectRef: ref,
		Payload: map[string]any{"scope": "PRINCIPAL"}})
}

var _ Recorder = (*RecordRecorder)(nil)
var _ Resumes = (*RecordRecorder)(nil)

// Denied has the shape of understanding/resume.Denials; this package does not import resume, so the composition in cmd/core is where the assignment is checked.
