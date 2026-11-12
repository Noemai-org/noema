package intentstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	prefix        = "intent store: "
	emitter       = "core-workflows" // what the orchestrator's login is bound to write (migration 0004)
	eventAdmitted = "INTENT_ADMITTED"
)

// ErrNotFound is returned by a Reader for an intent or an investigation that the login's tenant has no row for (and for another tenant's, which it cannot see).
var ErrNotFound = errors.New(prefix + "not found")

// Admitted is one admitted Intent Object as the store keeps it: the columns the keys use and the document as the gate admitted it.
type Admitted struct {
	TenantID      string
	IntentID      string
	Version       int
	Supersedes    string // empty for version 1
	IntentNode    string
	IntentHash    string // "sha256:" and 64 hex digits, as the gate computed it
	CorrelationID string
	JSON          []byte // the admitted document (JCS form)
}

// Ref names a stored intent's Record event.
type Ref struct {
	EventID string
	Seq     int64
	Hash    string
}

var (
	actorRe  = regexp.MustCompile(`^[a-z]+:[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	intentRe = regexp.MustCompile(`^int_[0-9A-Za-z_]{2,64}$`)
	hashRe   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	corrRe   = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
)

// Store saves admitted intents. Its Pool must log in as the tenant's own orchestrator login: the database, not this code, binds the row and the Record event to the
// tenant and to that power's emitter (migrations 0004, 0026).
type Store struct {
	Pool     *pgxpool.Pool
	Appender *chain.Appender
	Timeout  time.Duration // from configuration; each save has its own deadline
}

// Save appends INTENT_ADMITTED and stores the row in one transaction, the event first (REQ-AUD-001, EC-E16). actor is the principal the intent belongs to, "<kind>:<id>"
// from the authenticated session. An error means neither the event nor the row stands.
func (s *Store) Save(ctx context.Context, actor string, a Admitted) (Ref, error) {
	if s == nil || s.Pool == nil || s.Appender == nil || s.Timeout <= 0 {
		return Ref{}, errors.New(prefix + "the store needs a pool, an appender and a positive timeout")
	}
	switch {
	case !actorRe.MatchString(actor):
		return Ref{}, errors.New(prefix + "the actor is not <kind>:<id>")
	case !chain.ValidTenantID(a.TenantID):
		return Ref{}, errors.New(prefix + "the tenant id is not valid")
	case !intentRe.MatchString(a.IntentID) || a.Version < 1 || !hashRe.MatchString(a.IntentHash) || !corrRe.MatchString(a.CorrelationID) || a.IntentNode == "":
		return Ref{}, errors.New(prefix + "the admitted intent is incomplete")
	case !json.Valid(a.JSON) || len(a.JSON) == 0:
		return Ref{}, errors.New(prefix + "the admitted document is not JSON")
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Ref{}, fmt.Errorf("%sbegin: %w", prefix, err)
	}
	fail := func(err error) (Ref, error) {
		rb, stop := context.WithTimeout(context.Background(), s.Timeout)
		defer stop()
		_ = tx.Rollback(rb)
		return Ref{}, err
	}
	st, err := s.Appender.Append(ctx, tx, chain.Event{TenantID: a.TenantID, CorrelationID: a.CorrelationID, WrittenBy: emitter, Actor: actor, EventType: eventAdmitted,
		ObjectRef: a.IntentID, Payload: map[string]any{"intent_node": a.IntentNode, "intent_id": a.IntentID, "version": a.Version, "intent_hash": a.IntentHash}})
	if err != nil {
		return fail(err)
	}
	var sup any
	if a.Supersedes != "" {
		sup = a.Supersedes
	}
	if _, err := tx.Exec(ctx, `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`, a.TenantID, a.IntentID, a.Version, sup, a.IntentNode, a.IntentHash, a.CorrelationID, string(a.JSON)); err != nil {
		return fail(fmt.Errorf("%sinsert: %w", prefix, err))
	}
	if err := tx.Commit(ctx); err != nil {
		return Ref{}, fmt.Errorf("%scommit: %w", prefix, err)
	}
	return Ref{EventID: st.EventID, Seq: st.Seq, Hash: st.Hash}, nil
}

// Reader reads admitted intents. Its Pool is the tenant login of the reader's own power (Trust or Execution; the orchestrator can read too): the tenant is the
// login's, so another tenant's intent is not found rather than refused.
type Reader struct {
	Pool    *pgxpool.Pool
	Timeout time.Duration
}

const selectColumns = `SELECT tenant_id, intent_id, version, COALESCE(supersedes, ''), intent_node, intent_hash, correlation_id, json::text FROM admitted_intent`

func (r *Reader) one(ctx context.Context, where string, args ...any) (Admitted, error) {
	if r == nil || r.Pool == nil || r.Timeout <= 0 {
		return Admitted{}, errors.New(prefix + "the reader needs a pool and a positive timeout")
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	var a Admitted
	var doc string
	err := r.Pool.QueryRow(ctx, selectColumns+where, args...).Scan(&a.TenantID, &a.IntentID, &a.Version, &a.Supersedes, &a.IntentNode, &a.IntentHash, &a.CorrelationID, &doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admitted{}, ErrNotFound
	}
	if err != nil {
		return Admitted{}, fmt.Errorf("%sread: %w", prefix, err)
	}
	a.JSON = []byte(doc)
	return a, nil
}

// Get reads one version of an intent.
func (r *Reader) Get(ctx context.Context, intentID string, version int) (Admitted, error) {
	return r.one(ctx, ` WHERE intent_id = $1 AND version = $2`, intentID, version)
}

// Latest reads the highest version of the investigation's intent: the hash Execution compares at E3 (noema#448, IntentSource).
func (r *Reader) Latest(ctx context.Context, correlationID string) (Admitted, error) {
	return r.one(ctx, ` WHERE correlation_id = $1 ORDER BY version DESC LIMIT 1`, correlationID)
}
