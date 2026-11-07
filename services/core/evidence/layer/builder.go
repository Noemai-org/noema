package layer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	builderPrefix   = "evidence layer: "
	emitter         = "core-evidence"
	eventEvidence   = "EVIDENCE_READ"
	idEvidence      = "evr"
	idPayload       = "pay"
	hashPrefix      = "sha256:"
	tsLayout        = "2006-01-02T15:04:05.000000000Z"
	emptyBodyMarker = "read_class"
)

var rawHashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// IDSource makes the prefixed ULIDs of PERD 32.3.
type IDSource interface {
	ID(prefix string) (string, error)
}

// Observation is one read as the Evidence Layer is given it: what the adapter learned and what the pack's descriptor says about the
// source. The status is not in it; the builder assigns it (REQ-CTX-002).
type Observation struct {
	TenantID       string
	CorrelationID  string
	Source         string // the PackId of the evidence-source
	SourceRecordID string
	ReadBy         string // TRUST, OUTCOME, RESOLUTION or EXECUTION_REVALIDATION
	EffectiveAt    time.Time
	AuthorityClass string // AUTHORITATIVE_FOR:<fact>, CORROBORATING_FOR:<fact> or NONE, from the pack
	ResourceScope  string
	PrincipalScope string
	Sensitivity    string
	Read           Read
	Src            Source
	Payload        any    // the normalised response; nil for a read that returned none
	RawHash        string // "sha256:" of the raw response bytes; empty for a read that returned none
	// NoBytes says explicitly that no response bytes arrived (a timeout, a reset, a local refusal, an open circuit). A failed read with no normalised response carries
	// exactly one of RawHash (bytes arrived, even an empty body) and NoBytes, so SHA-256 of nothing is never invented for a body that was there.
	NoBytes bool
	// DataSource and Synthetic label evidence from a synthetic source (for example "stand-in", AGENT-25). They ride in the EVIDENCE_READ event;
	// an empty DataSource writes the event without a label.
	DataSource string
	Synthetic  bool
}

// Item is what the builder stored.
type Item struct {
	EvidenceID  string
	PayloadRef  string
	PayloadHash string
	Status      contracts.EnumsV1FactStatus
}

// Builder turns an Observation into a stored evidence item (REQ-CTX-001). Its Pool must be the tenant's own evidence login: the
// database, not this code, binds a write to the tenant and the power (migration 0008). The payload, the item and the EVIDENCE_READ
// Record event go in one transaction, so a failed append stores nothing (REQ-AUD-001, EC-E16).
type Builder struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
	IDs   IDSource
	Rec   *chain.Appender
	// RollbackTimeout bounds the cleanup after a failure or a cancelled context (A24). It comes from configuration; zero refuses every Record (AGENT-16).
	RollbackTimeout time.Duration
}

// Record stores the observation and returns the item with the status AssignStatus gave it.
func (b *Builder) Record(ctx context.Context, o Observation) (Item, error) {
	if _, ok := ctx.Deadline(); !ok {
		return Item{}, errors.New(builderPrefix + "the context needs a deadline")
	}
	if b.RollbackTimeout <= 0 {
		return Item{}, errors.New(builderPrefix + "the builder needs a positive rollback timeout from configuration")
	}
	sla := int64(o.Src.SourceLimit / time.Second)
	if sla < 1 {
		return Item{}, fmt.Errorf("%sthe source has no positive freshness window", builderPrefix)
	}
	if err := checkResponse(o); err != nil {
		return Item{}, err
	}
	now := b.Clock.Now()
	status := AssignStatus(o.Read, o.Src, now)
	payload := o.Payload
	if payload == nil {
		payload = map[string]any{emptyBodyMarker: string(o.Read.Class)} // a read that returned nothing still has a payload row
	}
	canon, err := crypto.CanonicalJSON(payload)
	if err != nil {
		return Item{}, fmt.Errorf("%scanonical payload: %w", builderPrefix, err)
	}
	sum := sha256.Sum256(canon)
	hash := hashPrefix + hex.EncodeToString(sum[:])
	raw := o.RawHash
	if raw == "" { // checkResponse allowed this only for a read that said no bytes arrived
		empty := sha256.Sum256(nil)
		raw = hashPrefix + hex.EncodeToString(empty[:])
	}
	evidenceID, err := b.IDs.ID(idEvidence)
	if err != nil {
		return Item{}, fmt.Errorf("%sevidence id: %w", builderPrefix, err)
	}
	ref, err := b.IDs.ID(idPayload)
	if err != nil {
		return Item{}, fmt.Errorf("%spayload id: %w", builderPrefix, err)
	}
	tx, err := b.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Item{}, fmt.Errorf("%sbegin: %w", builderPrefix, err)
	}
	if err := b.write(ctx, tx, o, row{evidenceID: evidenceID, ref: ref, hash: hash, raw: raw, canon: canon, sla: sla, status: status}, now); err != nil {
		return Item{}, rollbackWith(ctx, tx, err, b.RollbackTimeout)
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("%scommit %s: %w", builderPrefix, evidenceID, err)
	}
	return Item{EvidenceID: evidenceID, PayloadRef: ref, PayloadHash: hash, Status: status}, nil
}

// row is what Record computed before it opens the transaction.
type row struct {
	evidenceID, ref, hash, raw string
	canon                      []byte
	sla                        int64
	status                     contracts.EnumsV1FactStatus
}

// rollbackWith rolls the transaction back and returns the original error, joined with the rollback's own error if it failed (never swallowed).
// The rollback must still be tried when the caller's deadline has passed, so it runs detached from the caller's cancellation; and it has a
// deadline of its own, so that a stalled database cannot hold the connection after the answer is decided (AGENTS.md section 7, EC-E16).
func rollbackWith(ctx context.Context, tx pgx.Tx, cause error, limit time.Duration) error {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), limit)
	defer cancel()
	if err := tx.Rollback(rctx); err != nil {
		return errors.Join(cause, fmt.Errorf("%srollback: %w", builderPrefix, err))
	}
	return cause
}

// checkResponse refuses an observation whose evidence would be unprovable or contradictory: a normalised response must be a JSON object
// (evidence-payload.v1) and must carry the hash of the raw bytes it came from; a read that says it succeeded must carry a response, because a
// bodyless read must not become a known fact; a raw hash that is supplied must be well formed (a malformed 2xx body keeps the hash of the bytes
// received); and the authority class stored on the item must agree with the source's authority. A malformed read must carry the hash of the bytes received (SHA-256 of nothing if the body was empty). A failed read with no response carries the hash of the bytes it received or says explicitly that none arrived (NoBytes), and the event records which.
func checkResponse(o Observation) error {
	if err := checkAuthority(o); err != nil {
		return err
	}
	if o.Payload == nil {
		if o.Read.Class == ReadOK {
			return fmt.Errorf("%san OK read carries no response", builderPrefix)
		}
		switch {
		case o.RawHash != "" && o.NoBytes:
			return fmt.Errorf("%sa failed read cannot both carry a body hash and say no bytes arrived", builderPrefix)
		case o.RawHash == "" && !o.NoBytes:
			return fmt.Errorf("%sa failed read needs the hash of the bytes it received, or an explicit no-bytes", builderPrefix)
		case o.RawHash != "" && !rawHashRe.MatchString(o.RawHash):
			return fmt.Errorf("%sthe raw hash is malformed", builderPrefix)
		}
		return nil
	}
	if o.NoBytes {
		return fmt.Errorf("%sa read with a response cannot say no bytes arrived", builderPrefix)
	}
	if !rawHashRe.MatchString(o.RawHash) {
		return fmt.Errorf("%sthe response has no valid raw hash", builderPrefix)
	}
	canon, err := crypto.CanonicalJSON(o.Payload)
	if err != nil {
		return fmt.Errorf("%scanonical payload: %w", builderPrefix, err)
	}
	if len(canon) == 0 || canon[0] != '{' {
		return fmt.Errorf("%sthe normalised response must be a JSON object", builderPrefix)
	}
	return nil
}

// checkAuthority refuses a source authority and an authority class that disagree: the status is derived from one and the item stores the other.
func checkAuthority(o Observation) error {
	want := map[Authority]string{AuthAuthoritative: "AUTHORITATIVE_FOR:", AuthCorroborating: "CORROBORATING_FOR:"}
	switch o.Src.Authority {
	case AuthNone:
		if o.AuthorityClass != "NONE" {
			return fmt.Errorf("%sauthority NONE needs the class NONE", builderPrefix)
		}
	case AuthAuthoritative, AuthCorroborating:
		if !strings.HasPrefix(o.AuthorityClass, want[o.Src.Authority]) || len(o.AuthorityClass) == len(want[o.Src.Authority]) {
			return fmt.Errorf("%sthe authority class does not match the source's authority", builderPrefix)
		}
	default:
		return fmt.Errorf("%sunknown source authority", builderPrefix)
	}
	return nil
}

func (b *Builder) write(ctx context.Context, tx pgx.Tx, o Observation, r row, now time.Time) error {
	evidenceID, ref, hash, raw, sla, status := r.evidenceID, r.ref, r.hash, r.raw, r.sla, r.status
	if _, err := tx.Exec(ctx, `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at) VALUES ($1, $2, $3::jsonb, $4, $5, $6)`,
		ref, o.TenantID, string(r.canon), raw, o.Sensitivity, now); err != nil {
		return fmt.Errorf("%sinsert payload %s: %w", builderPrefix, ref, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at,
			freshness_sla_seconds, authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		evidenceID, o.TenantID, o.CorrelationID, o.Source, o.SourceRecordID, o.ReadBy, o.Read.ReadAt, o.EffectiveAt, sla, o.AuthorityClass,
		o.ResourceScope, o.PrincipalScope, o.Sensitivity, string(status), hash, ref, now); err != nil {
		return fmt.Errorf("%sinsert item %s: %w", builderPrefix, evidenceID, err)
	}
	event := map[string]any{"evidence_id": evidenceID, "source": o.Source, "source_record_id": o.SourceRecordID, "read_by": o.ReadBy,
		"fact_status": string(status), "payload_hash": hash, "read_at": o.Read.ReadAt.UTC().Format(tsLayout)} // hash only: never the payload
	event["body_received"] = !o.NoBytes // the Record says whether a body arrived, so a reader never has to guess from an empty hash
	if o.DataSource != "" {
		event["data_source"] = o.DataSource
	}
	if o.DataSource != "" || o.Synthetic { // the synthetic label never depends on the source having a name
		event["synthetic"] = o.Synthetic
	}
	if _, err := b.Rec.Append(ctx, tx, chain.Event{TenantID: o.TenantID, CorrelationID: o.CorrelationID, WrittenBy: emitter, Actor: emitter,
		EventType: eventEvidence, ObjectRef: evidenceID, Payload: event}); err != nil {
		return fmt.Errorf("%sappend %s: %w", builderPrefix, eventEvidence, err)
	}
	return nil
}
