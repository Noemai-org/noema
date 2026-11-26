package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/edge/events"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/publicstage"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/redaction"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// wire_events.go builds the events route of EDG-02 (REQ-API-001; INV-12, INV-21): GET /v1/conversations/{conversation_ref}/events as a stream of sse-event.v1 payloads, read from
// the Record directly, in chain order, as the tenant's own record_read login (forced row-level security, DEBT-04; the outbox of noema#693 is not needed). The mount line is the final
// main.go PR's. A conversation is an investigation (inv_...). What the Record holds today is the investigation's state changes, so the feed emits `progress` events with the public
// stage (publicstage.Of) and nothing else: no Record payload carries the content of a clarification or confirmation card or of a customer answer (see status/EDG-02-wire-events.md),
// and the feed never builds one from anything else. It never emits an internal state, an event payload, or an event of another principal or tenant.

// The settings of the route. Every one is required once the Record reader is configured and none has a default: they are policy numbers of the Architect and the Founders (AGENT-16).
const (
	EnvEventsPollMS                 = "NOEMA_EVENTS_POLL_MS"
	EnvEventsMaxStreamMS            = "NOEMA_EVENTS_MAX_STREAM_MS"
	EnvEventsWriteTimeoutMS         = "NOEMA_EVENTS_WRITE_TIMEOUT_MS"
	EnvEventsReadTimeoutMS          = "NOEMA_EVENTS_READ_TIMEOUT_MS"
	EnvEventsMaxEventBytes          = "NOEMA_EVENTS_MAX_EVENT_BYTES"
	EnvEventsMaxStreams             = "NOEMA_EVENTS_MAX_STREAMS"
	EnvEventsMaxStreamsPerPrincipal = "NOEMA_EVENTS_MAX_STREAMS_PER_PRINCIPAL"
	EnvEventsMaxBatch               = "NOEMA_EVENTS_MAX_BATCH"
)

// eventsSettings is the parsed form of the settings above.
type eventsSettings struct {
	poll, maxStream, writeTimeout, readTimeout        time.Duration
	maxEventBytes, maxStreams, perPrincipal, maxBatch int
}

// eventsBounds is the approved range of each setting, in the unit the setting names (A658, interim, relayed, not yet committed). They are resource limits, not safety thresholds; the
// values inside them are the deployment's and none has a default (AGENT-16).
var eventsBounds = map[string]struct{ min, max int64 }{
	EnvEventsPollMS:                 {2000, 60000},
	EnvEventsMaxStreamMS:            {60000, 900000},
	EnvEventsWriteTimeoutMS:         {1000, 30000},
	EnvEventsReadTimeoutMS:          {500, 10000},
	EnvEventsMaxEventBytes:          {1024, 1048576},
	EnvEventsMaxStreams:             {1, 1000},
	EnvEventsMaxStreamsPerPrincipal: {1, 10},
	EnvEventsMaxBatch:               {1, 500},
}

// positive reads one setting as a whole number inside its approved range. The error names the setting and never the value (A469).
func positive(env Env, name string) (int64, error) {
	b, ok := eventsBounds[name]
	if !ok {
		return 0, fmt.Errorf("cmd/core: %s has no approved range", name)
	}
	n, err := strconv.ParseInt(env.Getenv(name), 10, 64)
	if err != nil || n < b.min || n > b.max {
		return 0, fmt.Errorf("cmd/core: %s must be a whole number inside its approved range", name)
	}
	return n, nil
}

func loadEventsSettings(env Env) (eventsSettings, error) {
	var s eventsSettings
	ms := func(name string, into *time.Duration) error {
		n, err := positive(env, name)
		*into = time.Duration(n) * time.Millisecond
		return err
	}
	count := func(name string, into *int) error {
		n, err := positive(env, name)
		*into = int(n)
		return err
	}
	for _, f := range []func() error{
		func() error { return ms(EnvEventsPollMS, &s.poll) },
		func() error { return ms(EnvEventsMaxStreamMS, &s.maxStream) },
		func() error { return ms(EnvEventsWriteTimeoutMS, &s.writeTimeout) },
		func() error { return ms(EnvEventsReadTimeoutMS, &s.readTimeout) },
		func() error { return count(EnvEventsMaxEventBytes, &s.maxEventBytes) },
		func() error { return count(EnvEventsMaxStreams, &s.maxStreams) },
		func() error { return count(EnvEventsMaxStreamsPerPrincipal, &s.perPrincipal) },
		func() error { return count(EnvEventsMaxBatch, &s.maxBatch) },
	} {
		if err := f(); err != nil {
			return eventsSettings{}, err
		}
	}
	// The cross-checks fail closed and name both settings, never a value (A658).
	switch {
	case s.poll >= s.maxStream:
		return eventsSettings{}, fmt.Errorf("cmd/core: %s must be below %s", EnvEventsPollMS, EnvEventsMaxStreamMS)
	case s.readTimeout > s.poll:
		return eventsSettings{}, fmt.Errorf("cmd/core: %s must not exceed %s", EnvEventsReadTimeoutMS, EnvEventsPollMS)
	case s.perPrincipal > s.maxStreams:
		return eventsSettings{}, fmt.Errorf("cmd/core: %s must not exceed %s", EnvEventsMaxStreamsPerPrincipal, EnvEventsMaxStreams)
	}
	return s, nil
}

// buildEvents returns the events handler over the Record reader. With no Record reader it returns no handler and no error: the route stays unmounted. With one, a missing or bad
// setting is an error that names it. Nothing is dialled here.
func buildEvents(env Env, clk clock.Clock, records reconstruct.TenantDBs) (*events.Handler, error) {
	if records == nil {
		return nil, nil
	}
	s, err := loadEventsSettings(env)
	if err != nil {
		return nil, err
	}
	table, err := orchestrator.LoadTable()
	if err != nil {
		return nil, fmt.Errorf("cmd/core: the events route needs the transition table: %w", err)
	}
	feed := &recordFeed{read: readVerifiedChain(records), table: table, maxBatch: s.maxBatch, readTimeout: s.readTimeout}
	return events.New(events.Options{Feed: feed, Mask: redaction.New().MaskAccountNumbers, Wait: waitFor, Clock: clk, Poll: s.poll, MaxStream: s.maxStream,
		MaxEventBytes: s.maxEventBytes, WriteTimeout: s.writeTimeout, MaxStreams: s.maxStreams, MaxStreamsPerPrincipal: s.perPrincipal, MaxBatch: s.maxBatch})
}

// waitFor waits d or until ctx ends, returning ctx's error then.
func waitFor(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// chainReader returns the whole chain of the tenant the caller names, read and verified as that tenant's own login.
type chainReader func(ctx context.Context, tenantID string) ([]chain.Row, error)

// rollbackTimeout bounds the cleanup of a read on its own deadline: the request's may be spent.
const eventsRollbackTimeout = 5 * time.Second

// readVerifiedChain reads the tenant's chain in one read-only REPEATABLE READ transaction through the tenant's own pool and verifies it from genesis, as the reconstruction does:
// what is served is what was verified (INV-11). The pool is the tenant's alone (reconstruct.TenantDBs.For never falls back); the database binds the login to the tenant.
func readVerifiedChain(dbs reconstruct.TenantDBs) chainReader {
	return func(ctx context.Context, tenantID string) ([]chain.Row, error) {
		if _, ok := ctx.Deadline(); !ok {
			return nil, errors.New("cmd/core: the Record read needs a deadline")
		}
		db, err := dbs.For(tenantID)
		if err != nil {
			return nil, err
		}
		tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return nil, errors.New("cmd/core: the Record read could not begin")
		}
		defer func() {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), eventsRollbackTimeout)
			defer cancel()
			_ = tx.Rollback(rctx)
		}()
		rows, err := chain.ReadChain(ctx, tx)
		if err != nil {
			return nil, errors.New("cmd/core: the Record read failed")
		}
		if _, err := chain.VerifyRows(tenantID, rows); err != nil {
			return nil, errors.New("cmd/core: the Record chain did not verify")
		}
		return rows, nil
	}
}

var (
	conversationRef = regexp.MustCompile(`\Ainv_[0-9A-Za-z]{2,32}\z`)
	utcInstant      = regexp.MustCompile(`\A[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z\z`)
)

const (
	evtInput  = "INPUT_RECEIVED"
	evtChange = "INVESTIGATION_STATE_CHANGED"
)

// recordFeed is the events.Feed over the Record.
type recordFeed struct {
	read        chainReader
	table       *orchestrator.Table
	maxBatch    int
	readTimeout time.Duration
}

var _ events.Feed = (*recordFeed)(nil)

// Since returns the progress events of the caller's own investigation after a cursor. The cursor is the ordinal of an event among this investigation's progress events, never a
// position in the tenant's chain, so it tells nothing about other conversations. A conversation that is absent, another principal's or another tenant's, and a cursor beyond the
// last event, are the same answer: events.ErrNotFound. Any other failure is an error that names no cause.
func (f *recordFeed) Since(ctx context.Context, p session.Principal, conversationRef_ string, after int64) (events.Batch, error) {
	if f == nil || f.read == nil || f.table == nil || f.maxBatch < 1 || f.readTimeout <= 0 {
		return events.Batch{}, errors.New("cmd/core: the events feed is not configured")
	}
	if _, ok := ctx.Deadline(); !ok {
		return events.Batch{}, errors.New("cmd/core: the events feed needs a deadline")
	}
	if p.Tenant == "" || p.ID == "" || after < 0 {
		return events.Batch{}, events.ErrNotFound
	}
	if !conversationRef.MatchString(conversationRef_) {
		return events.Batch{}, events.ErrNotFound
	}
	rctx, cancel := context.WithTimeout(ctx, f.readTimeout)
	defer cancel()
	rows, err := f.read(rctx, p.Tenant)
	if err != nil {
		return events.Batch{}, err
	}
	return f.derive(rows, p.ID, conversationRef_, after)
}

type stateChange struct {
	at string
	to orchestrator.State
}

// derive builds the batch from a verified chain. It is a pure function of the rows, so the same Record gives the same events and the same cursors (INV-21).
func (f *recordFeed) derive(rows []chain.Row, principalID, ref string, after int64) (events.Batch, error) {
	corr := ""
	for _, r := range rows {
		if r.EventType == evtChange && r.ObjectRef == ref {
			corr = r.CorrelationID
			break
		}
	}
	if corr == "" {
		return events.Batch{}, events.ErrNotFound
	}
	owner := "customer:" + principalID
	inputs := 0
	var changes []stateChange
	for _, r := range rows {
		if r.CorrelationID != corr {
			continue
		}
		switch {
		case r.EventType == evtInput:
			if r.Actor != owner {
				return events.Batch{}, events.ErrNotFound // an input of another principal in this correlation: not this caller's conversation
			}
			inputs++
		case r.EventType == evtChange && r.ObjectRef == ref:
			c, err := readChange(r)
			if err != nil {
				return events.Batch{}, err
			}
			changes = append(changes, c)
		}
	}
	if inputs == 0 {
		return events.Batch{}, events.ErrNotFound
	}
	var all []events.Event
	var prev contracts.InvestigationResponseV1PublicStage
	for _, c := range changes {
		stage, err := publicstage.Of(c.to)
		if err != nil {
			return events.Batch{}, errors.New("cmd/core: a recorded state has no public stage")
		}
		if stage == prev {
			continue // two internal states of one public stage are one thing to tell the customer
		}
		prev = stage
		all = append(all, events.Event{Cursor: int64(len(all) + 1), Progress: &contracts.SseEventV1Progress{At: contracts.CommonV1Timestamp(c.at), State: stage}})
	}
	if after > int64(len(all)) {
		return events.Batch{}, events.ErrNotFound
	}
	rest := all[after:]
	done := len(changes) > 0 && len(f.table.Exits(changes[len(changes)-1].to)) == 0
	if len(rest) > f.maxBatch {
		rest, done = rest[:f.maxBatch], false
	}
	return events.Batch{Events: rest, Done: done}, nil
}

// readChange reads the target state and the instant of one INVESTIGATION_STATE_CHANGED row. A row that does not have them is an error, never a guess.
func readChange(r chain.Row) (stateChange, error) {
	var body struct {
		At string `json:"at"`
	}
	var payload struct {
		To string `json:"to"`
	}
	if r.Payload == nil || json.Unmarshal([]byte(r.Body), &body) != nil || json.Unmarshal([]byte(*r.Payload), &payload) != nil || !utcInstant.MatchString(body.At) || payload.To == "" {
		return stateChange{}, errors.New("cmd/core: a recorded state change cannot be read")
	}
	return stateChange{at: body.At, to: orchestrator.State(payload.To)}, nil
}
