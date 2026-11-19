package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Errors of the package. None carries a payload: an event's payload is data and may hold identifiers (AGENT-13).
var (
	// ErrInvalidEvent is returned for an event that is not well formed. The message says which field, never what it held.
	ErrInvalidEvent = errors.New("outbox: invalid event")
	// ErrNoDeadline is returned for a context without a deadline (AGENTS section 7, Go: a context deadline on every call).
	ErrNoDeadline = errors.New("outbox: the context needs a deadline")
	// ErrDuplicateEvent is returned when the outbox ID is already taken.
	ErrDuplicateEvent = errors.New("outbox: the outbox ID is already used")
	// ErrWrite is returned for any other refusal of the database; the message carries the SQLSTATE and the constraint name and nothing from the row.
	ErrWrite = errors.New("outbox: the database refused the event")
)

var (
	// outbox IDs are `obx_` and a ULID (PERD §32.3): 26 characters of Crockford's base32.
	idRe     = regexp.MustCompile(`^obx_[0-9A-HJKMNP-TV-Z]{26}$`)
	tenantRe = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`) // common.v1
	// a topic is two or more dotted lower-case words, as outcome.verify and recovery.requested are.
	topicRe = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
)

const maxTopic = 64

// Event is one event to be written with its state change.
type Event struct {
	OutboxID  string          // `obx_<ULID>`, made by platform/ids; the idempotency key of every delivery
	TenantID  string          // the tenant of the login that writes it (row security refuses any other)
	Topic     string          // for example `outcome.verify`; the dispatcher routes by it
	Payload   json.RawMessage // a JSON object
	CreatedAt time.Time       // from the clock source
}

// Validate refuses an event that is not well formed, without reading the database. Its messages name the field, never the value.
func (e Event) Validate() error {
	bad := func(what string) error { return fmt.Errorf("%w: %s", ErrInvalidEvent, what) }
	switch {
	case !idRe.MatchString(e.OutboxID):
		return bad("the outbox ID is not obx_ and 26 Crockford characters")
	case !tenantRe.MatchString(e.TenantID):
		return bad("the tenant ID is not of the common.v1 shape")
	case len(e.Topic) > maxTopic || !topicRe.MatchString(e.Topic):
		return bad("the topic is not dotted lower-case words of at most 64 characters")
	case e.CreatedAt.IsZero():
		return bad("the creation time is not set")
	}
	return validObject(e.Payload)
}

func validObject(p []byte) error {
	t := bytes.TrimSpace(p)
	if len(t) == 0 || t[0] != '{' || !json.Valid(t) {
		return fmt.Errorf("%w: the payload is not one JSON object", ErrInvalidEvent)
	}
	return nil
}

// Enqueue writes the event on the caller's transaction and does nothing else: it never begins, commits or rolls back. The event therefore commits with the state change
// that the same transaction makes, or not at all (REQ-VER-005, PERD §56). The caller's login must hold INSERT on outbox_event and be mapped to the event's tenant.
func Enqueue(ctx context.Context, tx pgx.Tx, e Event) error {
	if _, ok := ctx.Deadline(); !ok {
		return ErrNoDeadline
	}
	if err := e.Validate(); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at) VALUES ($1, $2, $3, $4::jsonb, $5)`,
		e.OutboxID, e.TenantID, e.Topic, []byte(e.Payload), e.CreatedAt.UTC())
	if err == nil {
		return nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		// A database message may quote the failing row, payload included. Only the code and the constraint name are kept.
		if pe.Code == "23505" && pe.ConstraintName == "outbox_event_pkey" {
			return fmt.Errorf("%w: %s", ErrDuplicateEvent, e.OutboxID)
		}
		return fmt.Errorf("%w: SQLSTATE %s, constraint %q", ErrWrite, pe.Code, pe.ConstraintName)
	}
	return fmt.Errorf("%w: %w", ErrWrite, err) // not a database refusal (a lost connection, a cancelled context): its text holds no row
}
