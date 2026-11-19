package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

var (
	// ErrConfig is returned by New for a configuration that cannot run safely. The budget and the timeout have no defaults (REQ-AGT-005, AGENT-16).
	ErrConfig = errors.New("outbox: invalid dispatcher configuration")
	// ErrNoHandler is the failure of an event whose topic has no handler. The event stays pending: nobody received it.
	ErrNoHandler = errors.New("outbox: no handler for the topic")
	// ErrDeliveryFailed is wrapped by the error RunOnce returns when at least one event was not delivered; Result.Failed lists them.
	ErrDeliveryFailed = errors.New("outbox: an event was not delivered")
)

// Delivery is what a handler is given. Its idempotency key is the outbox ID: a handler delivers at least once, so a consumer deduplicates by it (PERD §56).
type Delivery struct {
	OutboxID  string
	TenantID  string
	Topic     string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// IdempotencyKey is the key a consumer deduplicates by: the outbox ID, the same on every redelivery.
func (d Delivery) IdempotencyKey() string { return d.OutboxID }

// Handler delivers one event. It returns nil only when the event was delivered; any error leaves the event pending for the next pass.
type Handler interface {
	Deliver(ctx context.Context, d Delivery) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, d Delivery) error

// Deliver calls f.
func (f HandlerFunc) Deliver(ctx context.Context, d Delivery) error { return f(ctx, d) }

// DeliveryFailure is one event that was not delivered in a pass. Its text names the outbox ID and the topic and never the cause's text (which a handler may build from
// the payload); the cause is reachable with errors.Is and errors.As.
type DeliveryFailure struct {
	OutboxID string
	Topic    string
	Err      error
}

// Error names the event, not the cause.
func (f DeliveryFailure) Error() string {
	return fmt.Sprintf("outbox: event %s on topic %s was not delivered", f.OutboxID, f.Topic)
}

// Unwrap returns the cause.
func (f DeliveryFailure) Unwrap() error { return f.Err }

// Result is what a pass did, oldest event first.
type Result struct {
	Delivered []string // outbox IDs delivered and marked dispatched
	Failed    []DeliveryFailure
}

// Config configures a Dispatcher. Nothing has a default.
type Config struct {
	TenantID        string             // the one tenant this dispatcher serves; its database login is that tenant's orchestrator login
	Handlers        map[string]Handler // by topic
	MaxEvents       int                // events one pass may try: a bound, not a goal
	DeliveryTimeout time.Duration      // the deadline of one delivery and its mark
}

// Beginner starts a transaction; a *pgxpool.Pool is one.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Dispatcher delivers the pending events of one tenant, at least once.
type Dispatcher struct {
	db       Beginner
	clk      clock.Clock
	cfg      Config
	handlers map[string]Handler
}

// New returns a Dispatcher, or ErrConfig. The handler table is copied.
func New(db Beginner, clk clock.Clock, cfg Config) (*Dispatcher, error) {
	bad := func(what string) error { return fmt.Errorf("%w: %s", ErrConfig, what) }
	switch {
	case db == nil:
		return nil, bad("no database")
	case clk == nil:
		return nil, bad("no clock")
	case !tenantRe.MatchString(cfg.TenantID):
		return nil, bad("the tenant ID is not of the common.v1 shape")
	case len(cfg.Handlers) == 0:
		return nil, bad("no handlers")
	case cfg.MaxEvents <= 0:
		return nil, bad("MaxEvents must be positive")
	case cfg.DeliveryTimeout <= 0:
		return nil, bad("DeliveryTimeout must be positive")
	}
	handlers := make(map[string]Handler, len(cfg.Handlers))
	for topic, h := range cfg.Handlers {
		if h == nil {
			return nil, bad("a nil handler")
		}
		if len(topic) > maxTopic || !topicRe.MatchString(topic) {
			return nil, bad("a handler's topic is not dotted lower-case words")
		}
		handlers[topic] = h
	}
	return &Dispatcher{db: db, clk: clk, cfg: cfg, handlers: handlers}, nil
}

// RunOnce makes one pass. For each pending event of its tenant, oldest first, at most MaxEvents in all and each at most once in the pass, it opens a transaction, locks the
// row (SKIP LOCKED, so two dispatchers never hold one event), calls the topic's handler, and only if the handler returned nil sets dispatched_at and commits. An error from
// the handler, a missing handler, a death of the process or a failed commit leaves the event pending: it is delivered again by a later pass, with the same key.
//
// The error is nil when every tried event was delivered. A failure of an event is in Result.Failed and wraps ErrDeliveryFailed in the returned error; a failure of the
// database is returned as it is, with the Result so far. The context needs a deadline.
func (d *Dispatcher) RunOnce(ctx context.Context) (Result, error) {
	var res Result
	if _, ok := ctx.Deadline(); !ok {
		return res, ErrNoDeadline
	}
	tried := make([]string, 0, d.cfg.MaxEvents)
	for len(tried) < d.cfg.MaxEvents {
		id, outcome, err := d.one(ctx, tried)
		if err != nil {
			return res, err
		}
		if id == "" {
			break // nothing pending that this pass has not tried
		}
		tried = append(tried, id)
		if outcome.err != nil {
			res.Failed = append(res.Failed, DeliveryFailure{OutboxID: id, Topic: outcome.topic, Err: outcome.err})
			continue
		}
		res.Delivered = append(res.Delivered, id)
	}
	if len(res.Failed) == 0 {
		return res, nil
	}
	errs := []error{ErrDeliveryFailed}
	for _, f := range res.Failed {
		errs = append(errs, f)
	}
	return res, errors.Join(errs...)
}

type outcome struct {
	topic string
	err   error // the delivery's failure, if any (not a database fault)
}

// one handles the oldest pending event that is not in tried. It returns "" when there is none.
func (d *Dispatcher) one(ctx context.Context, tried []string) (string, outcome, error) {
	c, cancel := context.WithTimeout(ctx, d.cfg.DeliveryTimeout)
	defer cancel()
	tx, err := d.db.Begin(c)
	if err != nil {
		return "", outcome{}, fmt.Errorf("outbox: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			rb, rbCancel := context.WithTimeout(context.WithoutCancel(ctx), d.cfg.DeliveryTimeout)
			defer rbCancel()
			_ = tx.Rollback(rb)
		}
	}()
	var e Delivery
	var raw []byte
	err = tx.QueryRow(c, `SELECT outbox_id, tenant_id, topic, payload, created_at FROM outbox_event
		WHERE tenant_id = $1 AND dispatched_at IS NULL AND outbox_id <> ALL($2::text[])
		ORDER BY created_at, outbox_id LIMIT 1 FOR UPDATE SKIP LOCKED`, d.cfg.TenantID, tried).Scan(&e.OutboxID, &e.TenantID, &e.Topic, &raw, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", outcome{}, nil
	}
	if err != nil {
		return "", outcome{}, fmt.Errorf("outbox: select the next pending event: %w", err)
	}
	e.Payload = raw
	h, ok := d.handlers[e.Topic]
	if !ok {
		return e.OutboxID, outcome{topic: e.Topic, err: ErrNoHandler}, nil
	}
	if err := h.Deliver(c, e); err != nil {
		return e.OutboxID, outcome{topic: e.Topic, err: err}, nil
	}
	tag, err := tx.Exec(c, `UPDATE outbox_event SET dispatched_at = $1 WHERE outbox_id = $2 AND tenant_id = $3 AND dispatched_at IS NULL`, d.clk.Now().UTC(), e.OutboxID, d.cfg.TenantID)
	if err != nil {
		return e.OutboxID, outcome{}, fmt.Errorf("outbox: mark %s dispatched: %w", e.OutboxID, err)
	}
	if tag.RowsAffected() != 1 {
		return e.OutboxID, outcome{}, fmt.Errorf("outbox: marking %s dispatched changed %d rows", e.OutboxID, tag.RowsAffected())
	}
	if err := tx.Commit(c); err != nil {
		return e.OutboxID, outcome{}, fmt.Errorf("outbox: commit the mark of %s: %w", e.OutboxID, err)
	}
	committed = true
	return e.OutboxID, outcome{topic: e.Topic}, nil
}
