package chain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

const (
	errPrefix = "record: "
	// SchemaVersion is the record-event contract version this package writes (contracts/schemas/record-event.v2.json).
	SchemaVersion = "2.0"
	hashPrefix    = "sha256:"
	genesisSalt   = "noema:genesis:"
)

// Patterns from contracts/schemas/common.v1.json and record-event.v1.json: an event that does not match them is
// refused before it reaches the database.
var (
	tenantRe      = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`)
	correlationRe = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
	objectRefRe   = regexp.MustCompile(`^[a-z]{2,6}_[0-9A-Za-z_]{2,64}$`)
	writtenByRe   = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	eventTypeRe   = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// Event is what a power asks the Record to hold. The Record never acts on it (INV-11).
type Event struct {
	TenantID      string
	CorrelationID string
	WrittenBy     string // the power, upper-case: EXECUTION, TRUST, ...
	Actor         string // the power or service identity, plus the principal where relevant (PERD 32.1)
	EventType     string // PERD 32.2
	Kind          string // SECURITY_EVENT only
	Reason        string // GRANT_VOIDED and PROVIDER_FAILOVER only
	ObjectRef     string // names an object, never a free-text claim
	// Payload is the object the event refers to or details it; its canonical hash goes into payload_hash and the
	// object itself is stored beside the event, outside the hash, so the verifier can check one against the other.
	Payload any
}

// Stored is an appended event as the chain now holds it.
type Stored struct {
	EventID  string
	Seq      int64
	PrevHash string
	Hash     string
}

// IDSource makes Record event IDs (`recev_<ULID>`, PERD 32.3).
type IDSource interface {
	ID(prefix string) (string, error)
}

// Appender appends to the per-tenant hash chain inside the caller's transaction (REQ-AUD-001): if the append
// fails, the transaction fails and the action does not happen.
type Appender struct {
	Clock clock.Clock
	IDs   IDSource
}

// GenesisHash is the prev_hash of the first event of a tenant's chain: SHA-256("noema:genesis:" || tenant_id)
// (PERD 32.1), written in the contract's `sha256:` form.
func GenesisHash(tenantID string) string {
	sum := sha256.Sum256([]byte(genesisSalt + tenantID))
	return hashPrefix + hex.EncodeToString(sum[:])
}

// Append writes ev as the next event of its tenant's chain, in tx. It locks the chain head (one writer per tenant
// at a time), computes hash = SHA-256(prev_hash || JCS(event without hash)) and inserts through record_insert,
// which refuses an event that does not extend the head it locked.
func (a *Appender) Append(ctx context.Context, tx pgx.Tx, ev Event) (Stored, error) {
	if _, ok := ctx.Deadline(); !ok {
		return Stored{}, errors.New(errPrefix + "the context needs a deadline")
	}
	if err := validate(ev); err != nil {
		return Stored{}, err
	}
	payloadHash, payloadJSON, err := payloadOf(ev.Payload)
	if err != nil {
		return Stored{}, err
	}
	id, err := a.IDs.ID("recev")
	if err != nil {
		return Stored{}, fmt.Errorf("%sevent id: %w", errPrefix, err)
	}
	at := a.Clock.Now().UTC().Truncate(time.Millisecond)

	var seq int64
	var prev string
	if err := tx.QueryRow(ctx, `SELECT seq, hash FROM record_lock_head($1)`, ev.TenantID).Scan(&seq, &prev); err != nil {
		return Stored{}, fmt.Errorf("%slock head: %w", errPrefix, err)
	}
	body := Body(ev, id, seq+1, prev, payloadHash, at)
	hash, err := Hash(prev, body)
	if err != nil {
		return Stored{}, err
	}
	bodyJSON, err := crypto.CanonicalJSON(body)
	if err != nil {
		return Stored{}, fmt.Errorf("%scanonical body: %w", errPrefix, err)
	}
	if _, err := tx.Exec(ctx, `SELECT record_insert($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15::jsonb)`,
		ev.TenantID, id, seq+1, prev, hash, ev.WrittenBy, ev.EventType, ev.ObjectRef, payloadHash, ev.CorrelationID, at,
		SchemaVersion, ev.Actor, string(bodyJSON), payloadJSON); err != nil {
		return Stored{}, fmt.Errorf("%sinsert: %w", errPrefix, err)
	}
	return Stored{EventID: id, Seq: seq + 1, PrevHash: prev, Hash: hash}, nil
}

// Hash is SHA-256(prev_hash || JCS(event without hash)) in the contract's `sha256:` form (PERD 32.1). body must
// not contain a hash field.
func Hash(prevHash string, body map[string]any) (string, error) {
	if _, has := body["hash"]; has {
		return "", errors.New(errPrefix + "the hashed event cannot contain its own hash")
	}
	canon, err := crypto.CanonicalJSON(body)
	if err != nil {
		return "", fmt.Errorf("%scanonical event: %w", errPrefix, err)
	}
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write(canon)
	return hashPrefix + hex.EncodeToString(h.Sum(nil)), nil
}

// Body is the event as it is hashed and stored: every field of record-event.v2.json except `hash`.
func Body(ev Event, id string, seq int64, prev, payloadHash string, at time.Time) map[string]any {
	body := map[string]any{
		"schema_version": SchemaVersion,
		"tenant_id":      ev.TenantID,
		"correlation_id": ev.CorrelationID,
		"event_id":       id,
		"seq":            seq,
		"prev_hash":      prev,
		"written_by":     ev.WrittenBy,
		"actor":          ev.Actor,
		"event_type":     ev.EventType,
		"object_ref":     ev.ObjectRef,
		"payload_hash":   payloadHash,
		"at":             at.Format("2006-01-02T15:04:05.000Z"),
	}
	if ev.Kind != "" {
		body["kind"] = ev.Kind
	}
	if ev.Reason != "" {
		body["reason"] = ev.Reason
	}
	return body
}

// PayloadHash is the hash of the referenced object's canonical JSON, and that JSON; an event that refers to no object
// (nil) carries the hash of the empty object {} and no stored payload (PERD 32.1).
func PayloadHash(p any) (hash string, canonical any, err error) { return payloadOf(p) }

func payloadOf(p any) (hash string, js any, err error) {
	if p == nil {
		h, err := crypto.HashOf(map[string]any{})
		return h, nil, err
	}
	canon, err := crypto.CanonicalJSON(p)
	if err != nil {
		return "", nil, fmt.Errorf("%spayload: %w", errPrefix, err)
	}
	return crypto.Hash(canon), string(canon), nil
}

func validate(ev Event) error {
	switch {
	case !tenantRe.MatchString(ev.TenantID):
		return fmt.Errorf("%stenant_id %q does not match the contract", errPrefix, ev.TenantID)
	case !correlationRe.MatchString(ev.CorrelationID):
		return fmt.Errorf("%scorrelation_id %q does not match the contract", errPrefix, ev.CorrelationID)
	case !objectRefRe.MatchString(ev.ObjectRef):
		return fmt.Errorf("%sobject_ref %q does not match the contract", errPrefix, ev.ObjectRef)
	case !writtenByRe.MatchString(ev.WrittenBy):
		return fmt.Errorf("%swritten_by %q does not match the contract", errPrefix, ev.WrittenBy)
	case !eventTypeRe.MatchString(ev.EventType):
		return fmt.Errorf("%sevent_type %q does not match the contract", errPrefix, ev.EventType)
	case ev.Actor == "":
		return errors.New(errPrefix + "actor is required")
	case !eventTypes[ev.EventType]:
		return fmt.Errorf("%sevent_type %q is not a Record event type (PERD 32.2)", errPrefix, ev.EventType)
	case ev.Kind != "" && !securityKinds[ev.Kind]:
		return fmt.Errorf("%skind %q is not a security kind", errPrefix, ev.Kind)
	case ev.EventType == "GRANT_VOIDED" && !grantVoidReasons[ev.Reason]:
		return fmt.Errorf("%sreason %q is not a grant-void reason: an unknown reason is rejected", errPrefix, ev.Reason)
	case ev.EventType == "PROVIDER_FAILOVER" && !providerFailoverReasons[ev.Reason]:
		return fmt.Errorf("%sreason %q is not a provider-failover reason", errPrefix, ev.Reason)
	case (ev.EventType == "SECURITY_EVENT") != (ev.Kind != ""):
		return errors.New(errPrefix + "kind is required on SECURITY_EVENT and only there")
	case (ev.EventType == "GRANT_VOIDED" || ev.EventType == "PROVIDER_FAILOVER") != (ev.Reason != ""):
		return errors.New(errPrefix + "reason is required on GRANT_VOIDED and PROVIDER_FAILOVER and only there")
	}
	return nil
}
