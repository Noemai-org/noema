package trustport

// RES-02 (REQ-RES-002, INV-20; A642, A677, interim, relayed): the Record side of the freeze row. CONTRADICTION_FREEZE_SET and CONTRADICTION_FREEZE_CLEARED are appended as Trust's own
// events (core-trust), and a target's freeze state is the latest of the two for it, read back through the Record's one read path (record_read_chain_t, as the tenant's own login). Each
// append runs in a transaction of its own: the derived freeze (the classification) still freezes if the SET is lost, and a lost CLEARED leaves the target frozen, so both losses fail closed.
// This file decides nothing; the pure rules are trust/reconcile's.

import (
	"context"
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
	state := reconcile.FreezeNone
	for _, r := range rows {
		if r.ObjectRef != target {
			continue
		}
		switch r.EventType {
		case eventSet:
			state = reconcile.FreezeRecorded
		case eventCleared:
			state = reconcile.FreezeCleared
		}
	}
	return state, nil
}

// RecordFreezeSet appends CONTRADICTION_FREEZE_SET for the target.
func (f *FreezeRow) RecordFreezeSet(ctx context.Context, s reconcile.FreezeSet) error {
	if err := f.ready(); err != nil {
		return err
	}
	return f.append(ctx, s.TenantID, chain.Event{TenantID: s.TenantID, CorrelationID: s.CorrelationID, WrittenBy: freezeEmitter, Actor: freezeEmitter, EventType: eventSet, ObjectRef: s.Target,
		Payload: map[string]any{"ruleset_id": s.RulesetID, "rule_id": s.RuleID, "evidence": s.Evidence, "labels": s.Labels}})
}

// RecordFreezeCleared appends CONTRADICTION_FREEZE_CLEARED for the target when a freeze is recorded for it and the named decision is the tenant's and lifts it: an ESCALATE for an
// escalation, an ALLOW for a resolution. That the decision names the target is not checked here: the Record holds no link from a decision to a target (listed in the PR).
func (f *FreezeRow) RecordFreezeCleared(ctx context.Context, c reconcile.FreezeLift) error {
	if err := f.ready(); err != nil {
		return err
	}
	state, err := f.RecordedFreeze(ctx, c.TenantID, c.Target)
	if err != nil {
		return err
	}
	if state != reconcile.FreezeRecorded {
		return ErrNothingToClear
	}
	tx, err := f.c.Begin.Begin(ctx, c.TenantID)
	if err != nil {
		return fmt.Errorf("trustport: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
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
		Payload: map[string]any{"cleared_by": string(c.By), "decision_id": c.DecisionID}}); err != nil {
		return fmt.Errorf("trustport: append %s: %w", eventCleared, err)
	}
	return tx.Commit(ctx)
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
