package inputgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	emitter           = "core-edge"
	eventReceived     = "INPUT_RECEIVED"
	eventDeduplicated = "INPUT_DEDUPLICATED"
	eventSecurity     = "SECURITY_EVENT"
	kindReused        = "REQUEST_ID_REUSED"
)

// RecordRecorder appends the gateway's events to the Record as core-edge. Its Pool must log in as the tenant's core-edge emitter login (migration 0011,
// DEBT-07): the database, not this code, binds the write to the tenant and the emitter, and that login can do nothing but append. Each event goes in its
// own transaction, so a failed append stores nothing (REQ-AUD-001, EC-E16).
type RecordRecorder struct {
	Pool     *pgxpool.Pool
	Appender *chain.Appender
	Timeout  time.Duration // from configuration; each append has its own deadline
}

func (r *RecordRecorder) append(ctx context.Context, ev chain.Event) (chain.Stored, error) {
	if r == nil || r.Pool == nil || r.Appender == nil || r.Timeout <= 0 {
		return chain.Stored{}, errors.New(prefix + "the recorder needs a pool, an appender and a positive timeout")
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return chain.Stored{}, fmt.Errorf("%sbegin: %w", prefix, err)
	}
	st, err := r.Appender.Append(ctx, tx, ev)
	if err != nil {
		rb, stop := context.WithTimeout(context.Background(), r.Timeout)
		defer stop()
		_ = tx.Rollback(rb)
		return chain.Stored{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return chain.Stored{}, fmt.Errorf("%scommit: %w", prefix, err)
	}
	return st, nil
}

// event builds one event of the gateway. objectRef is the core-minted reference of the input the event is about (A184): the first INPUT_RECEIVED carries the one the gateway
// minted, and every later event of the same input carries that same one. It is never built from the request id or from anything else a client sent.
func (r *RecordRecorder) event(id Identity, corr, eventType, kind string, payload map[string]any, objectRef string) chain.Event {
	return chain.Event{TenantID: id.TenantID, CorrelationID: corr, WrittenBy: emitter, Actor: id.PrincipalKind + ":" + id.PrincipalID, EventType: eventType, Kind: kind,
		ObjectRef: objectRef, Payload: payload}
}

// Received appends INPUT_RECEIVED: channel, locale (when the client sent a locale_hint) and request_id as the reconstruction reads them, and the body's hash.
func (r *RecordRecorder) Received(ctx context.Context, id Identity, corr string, in Received) (Ref, error) {
	p := map[string]any{"channel": in.Channel, "request_id": in.RequestID, "input_hash": in.InputHash}
	if in.Locale != "" {
		p["locale"] = in.Locale
	}
	if in.ObjectRef == "" {
		return Ref{}, errors.New(prefix + "the input has no minted object reference")
	}
	st, err := r.append(ctx, r.event(id, corr, eventReceived, "", p, in.ObjectRef))
	if err != nil {
		return Ref{}, err
	}
	return Ref{EventID: st.EventID, Seq: st.Seq, Hash: st.Hash, ObjectRef: in.ObjectRef}, nil
}

// Deduplicated appends INPUT_DEDUPLICATED on the original input's correlation, naming the original event.
func (r *RecordRecorder) Deduplicated(ctx context.Context, id Identity, corr, requestID string, original Ref) error {
	if original.ObjectRef == "" {
		return errors.New(prefix + "the original input has no object reference")
	}
	_, err := r.append(ctx, r.event(id, corr, eventDeduplicated, "", map[string]any{"request_id": requestID, "original_event_id": original.EventID}, original.ObjectRef))
	return err
}

// Reused appends SECURITY_EVENT of kind REQUEST_ID_REUSED on the original input's correlation. It carries the new body's hash, never its text.
func (r *RecordRecorder) Reused(ctx context.Context, id Identity, corr, requestID, inputHash string, original Ref) error {
	if original.ObjectRef == "" {
		return errors.New(prefix + "the original input has no object reference")
	}
	_, err := r.append(ctx, r.event(id, corr, eventSecurity, kindReused, map[string]any{"request_id": requestID, "original_event_id": original.EventID, "input_hash": inputHash}, original.ObjectRef))
	return err
}

// RecordIndex finds an earlier input by reading the tenant's own chain through record_read_chain_t as the tenant's record_read login. There is no table of
// request ids: the Record is the only store (INV-30), so a lookup reads the tenant's INPUT_RECEIVED events and filters them in the database. The cost grows with the
// tenant's chain; a keyed lookup function is the fix and is recorded as debt in status/UND-02.md.
type RecordIndex struct {
	Pool    *pgxpool.Pool
	Clock   clock.Clock
	Window  time.Duration // REQ-INP-003: 24 hours, from configuration
	Timeout time.Duration
}

// Find returns the latest INPUT_RECEIVED of the principal for the request id that is not older than the window.
func (x *RecordIndex) Find(ctx context.Context, id Identity, requestID string) (Prior, bool, error) {
	if x == nil || x.Pool == nil || x.Clock == nil || x.Window <= 0 || x.Timeout <= 0 {
		return Prior{}, false, errors.New(prefix + "the index needs a pool, a clock, a positive window and a positive timeout")
	}
	ctx, cancel := context.WithTimeout(ctx, x.Timeout)
	defer cancel()
	rows, err := x.Pool.Query(ctx, `SELECT seq, event_id, hash, object_ref, correlation_id, body::text, payload::text FROM record_read_chain_t()
		WHERE event_type = $1 AND actor = $2 AND (payload::jsonb)->>'request_id' = $3 ORDER BY seq DESC`, eventReceived, id.PrincipalKind+":"+id.PrincipalID, requestID)
	if err != nil {
		return Prior{}, false, fmt.Errorf("%sread the Record: %w", prefix, err)
	}
	defer rows.Close()
	now := x.Clock.Now()
	for rows.Next() {
		var p Prior
		var body, payload string
		if err := rows.Scan(&p.Ref.Seq, &p.Ref.EventID, &p.Ref.Hash, &p.Ref.ObjectRef, &p.CorrelationID, &body, &payload); err != nil {
			return Prior{}, false, fmt.Errorf("%sread the Record: %w", prefix, err)
		}
		var b struct {
			At string `json:"at"`
		}
		if err := json.Unmarshal([]byte(body), &b); err != nil {
			return Prior{}, false, fmt.Errorf("%san event body cannot be read: %w", prefix, err)
		}
		at, err := time.Parse(time.RFC3339Nano, b.At)
		if err != nil {
			return Prior{}, false, fmt.Errorf("%san event has no valid time: %w", prefix, err)
		}
		if now.Sub(at) > x.Window {
			continue // older than the window: a new input, not a repeat
		}
		var pl struct {
			InputHash string `json:"input_hash"`
		}
		if err := json.Unmarshal([]byte(payload), &pl); err != nil {
			return Prior{}, false, fmt.Errorf("%san event payload cannot be read: %w", prefix, err)
		}
		p.InputHash = pl.InputHash // empty when an older event has none: it then matches no body, which is the safe answer
		return p, true, nil
	}
	if err := rows.Err(); err != nil {
		return Prior{}, false, fmt.Errorf("%sread the Record: %w", prefix, err)
	}
	return Prior{}, false, nil
}
