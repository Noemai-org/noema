package trustport

// RES-02 (REQ-RES-002, INV-20; A642, A677, interim, relayed): the Record side of the freeze row. CONTRADICTION_FREEZE_SET and CONTRADICTION_FREEZE_CLEARED are appended as Trust's own
// events (core-trust), and a target's freeze state is the latest of the two for it, read back through the Record's one read path (record_read_chain_t, as the tenant's own login). Each
// append runs in a transaction of its own: the derived freeze (the classification) still freezes if the SET is lost, and a lost CLEARED leaves the target frozen, so both losses fail closed.
// This file decides nothing; the pure rules are trust/reconcile's.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

const (
	freezeEmitter = "core-trust"
	eventSet      = "CONTRADICTION_FREEZE_SET"
	eventCleared  = "CONTRADICTION_FREEZE_CLEARED"
)

// ErrFreezeConfig is returned by the freeze row when it is not wired.
var ErrFreezeConfig = errors.New("trustport: the freeze row needs a transaction source, a reader and an appender")

// ErrNothingToClear is returned when a clearing is asked for a target whose latest freeze row is not a SET.
var ErrNothingToClear = errors.New("trustport: no recorded freeze to clear")

// ErrClearingDecision is returned when the decision named by a clearing does not exist for the tenant or is not of the kind that lifts a freeze.
var ErrClearingDecision = errors.New("trustport: the decision does not lift a freeze")

// ErrFreezeCorrelation is returned when a freeze is recorded with no correlation: a clearing is tied to the investigation the SET names.
var ErrFreezeCorrelation = errors.New("trustport: a freeze is recorded with the correlation of its investigation")

// FreezeRowConfig builds a FreezeRow. Begin opens a transaction as the tenant's own Trust login; Reads gives the tenant's Record read login (nil: no reader, the answer is unknown).
type FreezeRowConfig struct {
	Begin    TxBeginner
	Reads    func(tenantID string) chain.Queryer
	Appender *chain.Appender
}

// FreezeRow implements reconcile.FreezeRecorder and reconcile.FreezeReader over the Record.
type FreezeRow struct{ c FreezeRowConfig }

var (
	_ reconcile.FreezeRecorder = (*FreezeRow)(nil)
	_ reconcile.FreezeReader   = (*FreezeRow)(nil)
)

// NewFreezeRow returns a FreezeRow.
func NewFreezeRow(c FreezeRowConfig) *FreezeRow { return &FreezeRow{c: c} }

func (f *FreezeRow) ready() error {
	if f == nil || f.c.Begin == nil || f.c.Reads == nil || f.c.Appender == nil {
		return ErrFreezeConfig
	}
	return nil
}

// RecordedFreeze reads the tenant's chain and returns the state the latest freeze row of the target gives. Any failure is an error, which reconcile.ReadFreeze turns into FreezeUnknown.
func (f *FreezeRow) RecordedFreeze(ctx context.Context, tenantID, target string) (reconcile.RecordedFreeze, error) {
	if err := f.ready(); err != nil {
		return reconcile.FreezeUnknown, err
	}
	q := f.c.Reads(tenantID)
	if q == nil {
		return reconcile.FreezeUnknown, fmt.Errorf("trustport: no Record reader for the tenant")
	}
	rows, err := chain.ReadChain(ctx, q)
	if err != nil {
		return reconcile.FreezeUnknown, fmt.Errorf("trustport: read the freeze row: %w", err)
	}
	state, _, err := freezeOf(rows, target)
	return state, err
}

// freezeOf gives the state the latest freeze row of the target gives and the SET it follows, if any. A row that does not name the target it is recorded for is an error (A756): the answer
// is then unknown, which is frozen.
func freezeOf(rows []chain.Row, target string) (reconcile.RecordedFreeze, chain.Row, error) {
	state := reconcile.FreezeNone
	var set chain.Row
	for _, r := range rows {
		if r.ObjectRef != target || (r.EventType != eventSet && r.EventType != eventCleared) {
			continue
		}
		if !payloadNamesTarget(r, target) {
			return reconcile.FreezeUnknown, chain.Row{}, errFreezeRow
		}
		if r.EventType == eventSet {
			state, set = reconcile.FreezeRecorded, r
		} else {
			state = reconcile.FreezeCleared
		}
	}
	return state, set, nil
}

var errFreezeRow = errors.New("trustport: a freeze row does not name the target it is recorded for")

// payloadNamesTarget reports that the row's payload carries a target_ref equal to the target.
func payloadNamesTarget(r chain.Row, target string) bool {
	if r.Payload == nil {
		return false
	}
	var p struct {
		TargetRef string `json:"target_ref"`
	}
	if err := json.Unmarshal([]byte(*r.Payload), &p); err != nil {
		return false
	}
	return p.TargetRef == target
}

// RecordFreezeSet appends CONTRADICTION_FREEZE_SET for the target.
func (f *FreezeRow) RecordFreezeSet(ctx context.Context, s reconcile.FreezeSet) error {
	if err := f.ready(); err != nil {
		return err
	}
	if s.CorrelationID == "" {
		return ErrFreezeCorrelation // the SET names the investigation it belongs to: a SET that names none is not written
	}
	return f.append(ctx, s.TenantID, chain.Event{TenantID: s.TenantID, CorrelationID: s.CorrelationID, WrittenBy: freezeEmitter, Actor: freezeEmitter, EventType: eventSet, ObjectRef: s.Target,
		Payload: map[string]any{"target_ref": s.Target, "ruleset_id": s.RulesetID, "rule_id": s.RuleID, "evidence": s.Evidence, "labels": s.Labels}})
}

// RecordFreezeCleared appends CONTRADICTION_FREEZE_CLEARED for the target when a freeze is recorded for it and the named decision lifts it: an ESCALATE for an escalation, an ALLOW for
// a resolution, of the SAME investigation as the recorded SET (the same correlation) and recorded after it (SR on #1035). The Decision Record has no target member, so the link is the
// correlation and the order on the chain. Two clearings at once are serialised on the target by a transaction-scoped advisory lock, so one CLEARED is written and the other reads the
// clearing and is refused.
func (f *FreezeRow) RecordFreezeCleared(ctx context.Context, c reconcile.FreezeLift) error {
	if err := f.ready(); err != nil {
		return err
	}
	tx, err := f.c.Begin.Begin(ctx, c.TenantID)
	if err != nil {
		return fmt.Errorf("trustport: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, c.TenantID+"/freeze/"+c.Target); err != nil {
		return fmt.Errorf("trustport: lock the freeze row: %w", err)
	}
	q := f.c.Reads(c.TenantID)
	if q == nil {
		return fmt.Errorf("trustport: no Record reader for the tenant")
	}
	rows, err := chain.ReadChain(ctx, q)
	if err != nil {
		return fmt.Errorf("trustport: read the freeze row: %w", err)
	}
	state, set, err := freezeOf(rows, c.Target)
	if err != nil {
		return err
	}
	if state != reconcile.FreezeRecorded {
		return ErrNothingToClear
	}
	if !decisionFollows(rows, set, c.DecisionID) {
		return ErrClearingDecision
	}
	var outcome, correlation string
	err = tx.QueryRow(ctx, `SELECT outcome, json ->> 'correlation_id' FROM decision_record WHERE tenant_id = $1 AND decision_id = $2`, c.TenantID, c.DecisionID).Scan(&outcome, &correlation)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrClearingDecision
	}
	if err != nil {
		return fmt.Errorf("trustport: read the decision: %w", err)
	}
	if (c.By == reconcile.ClearedByEscalation && outcome != "ESCALATE") || (c.By == reconcile.ClearedByResolution && outcome != "ALLOW") {
		return ErrClearingDecision
	}
	if _, err := f.c.Appender.Append(ctx, tx, chain.Event{TenantID: c.TenantID, CorrelationID: correlation, WrittenBy: freezeEmitter, Actor: freezeEmitter, EventType: eventCleared, ObjectRef: c.Target,
		Payload: map[string]any{"target_ref": c.Target, "cleared_by": string(c.By), "decision_id": c.DecisionID}}); err != nil {
		return fmt.Errorf("trustport: append %s: %w", eventCleared, err)
	}
	return tx.Commit(ctx)
}

// decisionFollows reports that the Record holds a DECISION_MADE for the decision, after the SET and of the SET's own correlation.
func decisionFollows(rows []chain.Row, set chain.Row, decisionID string) bool {
	for _, r := range rows {
		if r.EventType != "DECISION_MADE" || r.Seq <= set.Seq || r.CorrelationID != set.CorrelationID || r.Payload == nil {
			continue
		}
		var p struct {
			DecisionID string `json:"decision_id"`
		}
		if json.Unmarshal([]byte(*r.Payload), &p) == nil && p.DecisionID == decisionID {
			return true
		}
	}
	return false
}

func (f *FreezeRow) append(ctx context.Context, tenantID string, ev chain.Event) error {
	tx, err := f.c.Begin.Begin(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("trustport: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := f.c.Appender.Append(ctx, tx, ev); err != nil {
		return fmt.Errorf("trustport: append %s: %w", ev.EventType, err)
	}
	return tx.Commit(ctx)
}
