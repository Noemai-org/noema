package reconstruct

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// rollbackTimeout bounds the cleanup of a request. The rollback must still be tried when the request's own deadline has passed,
// so it runs on a context detached from that deadline, and never without a limit: a stalled database must not hold the request
// after its answer has been decided (review of noema#107).
const rollbackTimeout = 5 * time.Second

// SchemaVersion is the version of the reconstruction envelope, as in the contract's approved fixtures
// (contracts/fixtures/valid/reconstruction.v3/); the events inside carry record-event "3.0".
const SchemaVersion = "1.0"

// Errors Build returns. The handler maps each to one code of contracts/schemas/errors.v2.json.
var (
	// ErrIntegrity: the chain did not verify, so nothing is served (409 RECORD_INTEGRITY_FAILURE, INV-11).
	ErrIntegrity = errors.New("reconstruct: the Record chain did not verify")
	// ErrNotFound: the tenant's Record holds no event of this correlation.
	ErrNotFound = errors.New("reconstruct: no Record event for this correlation")
	// ErrUnprojectable: a verified event's object breaks the reconstruction contract; refused, never served partially.
	ErrUnprojectable = errors.New("reconstruct: a Record object cannot be served under the contract")
)

// TxBeginner is what Build needs: a pool whose connections run as record_read (migration 0003).
type TxBeginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Build returns the reconstruction of one correlation as canonical JSON (REQ-AUD-003). In one read-only REPEATABLE READ
// transaction it reads the tenant's whole chain once, verifies it from genesis, and builds the body from those same
// rows, so what is served is exactly what was verified. Every element comes from Record events and the payloads
// stored with them, and cites its record_event_id. The same Record gives the same bytes (INV-10, INV-21): the body
// holds nothing from the clock, the request or the chain beyond this correlation's last event.
func Build(ctx context.Context, db TxBeginner, tenantID, correlationID string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("reconstruct: the context needs a deadline")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("reconstruct: begin: %w", err)
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rctx)
	}()
	rows, err := chain.ReadChain(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	if _, err := chain.VerifyRows(tenantID, rows); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrIntegrity, err)
	}
	var span []chain.Row
	for _, r := range rows {
		if r.CorrelationID == correlationID {
			span = append(span, r)
		}
	}
	if len(span) == 0 {
		return nil, ErrNotFound
	}
	body, err := assemble(tenantID, correlationID, span)
	if err != nil {
		var pe *errProjection
		if errors.As(err, &pe) {
			return nil, fmt.Errorf("%w: %w", ErrUnprojectable, err)
		}
		return nil, err
	}
	return body, nil
}

func decode(s string) (any, error) {
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func assemble(tenantID, correlationID string, span []chain.Row) ([]byte, error) {
	events := make([]stored, 0, len(span))
	contractEvents := make([]any, 0, len(span))
	timeline := make([]any, 0, len(span))
	for _, r := range span {
		b, err := decode(r.Body)
		if err != nil {
			return nil, fmt.Errorf("reconstruct: event %s body: %w", r.EventID, err)
		}
		body, ok := b.(map[string]any)
		if !ok {
			return nil, projErr("event %s body is not an object", r.EventID)
		}
		payload := map[string]any{}
		if r.Payload != nil {
			p, err := decode(*r.Payload)
			if err != nil {
				return nil, fmt.Errorf("reconstruct: event %s payload: %w", r.EventID, err)
			}
			if m, ok := p.(map[string]any); ok {
				payload = m
			}
		}
		at, _ := body["at"].(string)
		kind, _ := body["kind"].(string)
		ev := stored{id: r.EventID, eventType: r.EventType, objectRef: r.ObjectRef, writtenBy: r.WrittenBy, kind: kind, at: at, body: body, payload: payload}
		events = append(events, ev)
		// The record-event.v3 event is the hashed body plus its hash.
		full := make(map[string]any, len(body)+1)
		for k, v := range body {
			full[k] = v
		}
		full["hash"] = r.Hash
		contractEvents = append(contractEvents, full)
		timeline = append(timeline, map[string]any{"record_event_id": r.EventID, "at": at, "event_type": r.EventType, "text": timelineText(ev)})
	}
	items, err := project(events)
	if err != nil {
		return nil, err
	}
	bySection := map[string][]any{}
	for _, it := range items {
		bySection[it.section] = append(bySection[it.section], it.value)
	}
	sections := make([]any, 0, len(sectionOrder))
	for _, s := range sectionOrder {
		list := bySection[s]
		if list == nil {
			list = []any{}
		}
		sections = append(sections, map[string]any{"kind": s, "items": list})
	}
	// The head is this correlation's last event: its hash commits to every event before it, all of which verified, and
	// it does not move when other correlations append later, so the body is reproducible.
	last := span[len(span)-1]
	doc := map[string]any{
		"schema_version": SchemaVersion,
		"tenant_id":      tenantID,
		"correlation_id": correlationID,
		"chain":          map[string]any{"verified": true, "head_event_id": last.EventID, "head_hash": last.Hash},
		"events":         contractEvents,
		"sections":       sections,
		"timeline":       timeline,
	}
	out, err := crypto.CanonicalJSON(doc)
	if err != nil {
		return nil, fmt.Errorf("reconstruct: canonical json: %w", err)
	}
	return out, nil
}
