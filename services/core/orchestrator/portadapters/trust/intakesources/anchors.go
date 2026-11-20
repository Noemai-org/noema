package intakesources

import (
	"context"
	"fmt"
	"maps"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

// AnchorEvent says where an anchor event is in the sealed snapshot's lists: the list entity, and the field of its rows that holds the instant the event happened.
type AnchorEvent struct {
	Entity string
	Field  string
}

// Anchors implements intakeadapters.Anchors over the sealed snapshot's lists (ruling A345 S3, A438): the time of a clock's anchor event is read from the list that records it, in the
// snapshot the decision rests on. The pack's anchor event names and the list entity and field behind each are given by the composition; this package names none.
type Anchors struct {
	lists  EvidenceLists
	events map[string]AnchorEvent
}

var _ intakeadapters.Anchors = (*Anchors)(nil)

// NewAnchors builds the reader. A configuration that cannot read a list, no event, an event with no name, entity or field, or an entity the lists do not serve is ErrConfig.
func NewAnchors(lists EvidenceLists, events map[string]AnchorEvent) (*Anchors, error) {
	if err := lists.valid(); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("%w: the anchors need at least one event", ErrConfig)
	}
	for name, ev := range events {
		if _, served := lists.Entities[ev.Entity]; name == "" || ev.Field == "" || !served {
			return nil, fmt.Errorf("%w: an anchor event needs a name, a field and a list entity the lists serve", ErrConfig)
		}
	}
	return &Anchors{lists: lists.own(), events: maps.Clone(events)}, nil
}

// AnchorTime gives the time of the anchor event in Unix seconds: the earliest instant of the event's field over the rows of its list in the sealed snapshot, and happened=true. A complete
// list with no row is happened=false (NOT_STARTED, A345 S3). Anything that cannot be told is an error and never happened=false (INV-19): the snapshot, the item, the payload or the list
// cannot be had as ListRows reads them, or a row has no usable instant. An event this reader was not given is ErrNoSource, with nothing read.
func (a *Anchors) AnchorTime(ctx context.Context, tenantID, snapshotID, anchorEvent string) (int64, bool, error) {
	if a == nil {
		return 0, false, ErrConfig
	}
	if err := checkSnapshot(tenantID, snapshotID); err != nil {
		return 0, false, err
	}
	ev, known := a.events[anchorEvent]
	if !known {
		return 0, false, fmt.Errorf("%w: the anchor event %s", ErrNoSource, anchorEvent)
	}
	items, err := a.lists.Items.Items(ctx, tenantID, snapshotID)
	if err != nil {
		return 0, false, err
	}
	rows, err := a.lists.rows(ctx, tenantID, items, ev.Entity)
	if err != nil {
		return 0, false, err
	}
	return earliest(rows, ev.Field)
}

// earliest is the smallest instant of the field over the rows. A row that has no instant in it, or one before second one, makes the whole answer an error: a clock is never started from
// a time nobody read, and never left unstarted because one row could not be read.
func earliest(rows []predicate.Row, field string) (int64, bool, error) {
	var first int64
	for i, row := range rows {
		v, ok := row[field]
		if !ok || v.Kind != predicate.KindTimestamp || v.I < 1 {
			return 0, false, fmt.Errorf("%w: a row of the list has no usable %s", ErrBadAnswer, field)
		}
		if i == 0 || v.I < first {
			first = v.I
		}
	}
	return first, len(rows) > 0, nil
}
