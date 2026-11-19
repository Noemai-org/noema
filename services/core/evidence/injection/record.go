package injection

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	emitter       = "core-evidence" // what the Evidence Layer's login is bound to write (migration 0004), as for the item builder
	eventSecurity = "SECURITY_EVENT"
	kindInjection = "INJECTION_DETECTED"
)

var (
	// nameRe is the shape of a field name. A name is chosen by the caller from typed descriptors, never from the enterprise string; a value that reached here as a name is refused
	// and not recorded (AGENT-13).
	nameRe = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)
	packRe = regexp.MustCompile(`^[a-z][a-z_.]{2,63}@[1-9][0-9]{0,3}$`)
)

// buildEvent makes the Record event for a Result: field names, pattern ids and the pack's identity, never a value. A Result with no finding is not an event.
func buildEvent(s Subject, r Result) (chain.Event, error) {
	if len(r.Findings) == 0 {
		return chain.Event{}, errors.New(prefix + "an event needs at least one finding")
	}
	if !packRe.MatchString(r.Pack) {
		return chain.Event{}, errors.New(prefix + "the pack identity is not <id>@<version>")
	}
	findings := make([]any, 0, len(r.Findings))
	for _, f := range r.Findings {
		if !nameRe.MatchString(f.Field) {
			return chain.Event{}, errors.New(prefix + "a field name is not an identifier")
		}
		if !idRe.MatchString(f.PatternID) {
			return chain.Event{}, errors.New(prefix + "a pattern id is not an identifier")
		}
		findings = append(findings, map[string]any{"field": f.Field, "pattern_id": f.PatternID})
	}
	truncated := make([]any, 0, len(r.TruncatedFields))
	for _, n := range r.TruncatedFields {
		if !nameRe.MatchString(n) {
			return chain.Event{}, errors.New(prefix + "a truncated field name is not an identifier")
		}
		truncated = append(truncated, n)
	}
	return chain.Event{TenantID: s.TenantID, CorrelationID: s.CorrelationID, WrittenBy: emitter, Actor: emitter, EventType: eventSecurity, Kind: kindInjection, ObjectRef: s.ObjectRef,
		Payload: map[string]any{"pack": r.Pack, "findings": findings, "truncated_fields": truncated}}, nil
}

// RecordRecorder appends the advisory event as the Evidence Layer's emitter, one transaction per event. Its Pool must log in as the tenant's own evidence login: the database, not this
// code, binds the event to the tenant and to that power's emitter (migration 0004). The append is the whole of the work: nothing is stored unless it commits (EC-E16).
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

// Record appends SECURITY_EVENT of kind INJECTION_DETECTED for a Result that has a finding.
func (r *RecordRecorder) Record(ctx context.Context, s Subject, res Result) error {
	if err := r.ready(); err != nil {
		return err
	}
	ev, err := buildEvent(s, res)
	if err != nil {
		return err
	}
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
