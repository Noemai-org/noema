package trustpg2

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

// emitter is the Record identity of the Trust power (PERD v1.17 section 32.1). The confirmation store is Trust's.
const emitter = "core-trust"

// ErrNoAppender is returned by RecordEvents with no appender: nothing is appended, so the store refuses to commit (fail closed, EC-E16).
var ErrNoAppender = errors.New("trustpg2: the Record appender is not wired")

// RecordEvents is the store's RequestEvents over the Record (A710, A724; REQ-TRU-009; PERD 32.4: the Record first). It appends in the transaction the store passes it, so the row and
// the event commit together or not at all.
type RecordEvents struct {
	Appender *chain.Appender
}

// Received appends CONFIRMATION_RECEIVED. The actor is the answerer with the kind the session gave (INV-12): the kind is a principal kind of the Record's actors table, and the
// appender refuses any other.
func (r RecordEvents) Received(ctx context.Context, tx pgx.Tx, e ReceivedEvent) error {
	if r.Appender == nil {
		return ErrNoAppender
	}
	_, err := r.Appender.Append(ctx, tx, chain.Event{TenantID: e.TenantID, CorrelationID: e.CorrelationID, WrittenBy: emitter, Actor: e.ApproverKind + ":" + e.ApproverID,
		EventType: "CONFIRMATION_RECEIVED", ObjectRef: e.RequestID,
		Payload: map[string]any{"confirmation_request_id": e.RequestID, "confirmation_id": e.EventID, "accepted": e.Accepted}})
	if err != nil {
		return fmt.Errorf("append CONFIRMATION_RECEIVED: %w", err)
	}
	return nil
}

// Expired appends CONFIRMATION_EXPIRED. No authenticated person causes an expiry, so the actor is the emitter's own identity.
func (r RecordEvents) Expired(ctx context.Context, tx pgx.Tx, e ExpiredEvent) error {
	if r.Appender == nil {
		return ErrNoAppender
	}
	_, err := r.Appender.Append(ctx, tx, chain.Event{TenantID: e.TenantID, CorrelationID: e.CorrelationID, WrittenBy: emitter, Actor: emitter,
		EventType: "CONFIRMATION_EXPIRED", ObjectRef: e.RequestID, Payload: map[string]any{"confirmation_request_id": e.RequestID}})
	if err != nil {
		return fmt.Errorf("append CONFIRMATION_EXPIRED: %w", err)
	}
	return nil
}
