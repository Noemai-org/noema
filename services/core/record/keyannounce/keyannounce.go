package keyannounce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

const errPrefix = "keyannounce: "

// EventType is the Record event type of an announced key registration (PERD 32.2, control plane).
const EventType = "KEY_ROTATED"

// Emitter is the identity that writes and signs the announcement (DEC-2026-11-01 D2a). With no administrator session behind a start-up announcement, the principal
// (actor) is the emitter's own identity, as the event contract allows for an event no authenticated person caused.
const Emitter = "admin"

// keyIDRe is the shape of a key ID (invariant K1, D4). Only a key ID of this shape is announced, so a value that is not one never reaches a payload.
var keyIDRe = regexp.MustCompile(`^[a-z][a-z-]*\.ed25519\.[1-9][0-9]*$`)

// KeyRegistry lists the key IDs of every signing_key row, whatever its status: a retired or revoked key was registered, and its registration reaches the Record too.
type KeyRegistry interface {
	KeyIDs(ctx context.Context) ([]string, error)
}

// PoolKeys is a KeyRegistry over a login that can SELECT from signing_key (migrations 0013 and 0019 grant it to the power logins, record_append and record_verify). The
// admin emitter's own login cannot.
type PoolKeys struct{ Q chain.Queryer }

// KeyIDs returns the key IDs in (identity, version) order, the order the events are appended in. Versions compare as integers (D4).
func (p PoolKeys) KeyIDs(ctx context.Context) ([]string, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New(errPrefix + "the context needs a deadline")
	}
	rows, err := p.Q.Query(ctx, `SELECT key_id FROM signing_key ORDER BY identity, version`)
	if err != nil {
		return nil, fmt.Errorf("%sread signing_key: %w", errPrefix, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("%sscan signing_key: %w", errPrefix, err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%sread signing_key: %w", errPrefix, err)
	}
	return out, nil
}

// TxBeginner is a pool whose connections log in as one tenant's admin emitter login (a member of record_emitter, bound to the emitter admin by migration 0011).
type TxBeginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Tenant is the connections the announcement of one tenant's chain needs. The database binds each login to its tenant (DEBT-04), so a login of another tenant reads and writes
// nothing of this one.
type Tenant struct {
	ID     string
	Admin  TxBeginner    // the tenant's login bound to the admin emitter
	Reader chain.Queryer // the tenant's record_read login
}

// Announcer appends the announcements. Its Appender's Signers must hold the admin signer; any other identity's signer is never used.
type Announcer struct {
	Appender *chain.Appender
	Keys     KeyRegistry
}

// Announce appends one KEY_ROTATED to the tenant's chain for every signing_key version that has none there, in one transaction, and returns the key IDs it announced. A key
// counts as announced only by a KEY_ROTATED written by admin whose payload names its key_id: another emitter's event does not stand in for it.
//
// It is exactly-once per key and chain: it reads first, and only when something is missing takes a per-tenant lock, reads again under it and appends. A second start-up, or a
// concurrent one, finds every key announced and appends nothing. Any failure is returned, the transaction is rolled back and nothing is kept: the caller refuses to start
// (fail closed, the Record first, PERD 32.4). The key IDs are announced, never key material (D4).
func (a *Announcer) Announce(ctx context.Context, t Tenant) ([]string, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New(errPrefix + "the context needs a deadline")
	}
	if a == nil || a.Appender == nil || a.Keys == nil || t.Admin == nil || t.Reader == nil {
		return nil, errors.New(errPrefix + "the announcer, its appender, key registry and the tenant's connections are all required")
	}
	if !chain.ValidTenantID(t.ID) {
		return nil, fmt.Errorf("%stenant ID %q does not match the contract", errPrefix, t.ID)
	}
	missing, err := a.missing(ctx, t)
	if err != nil || len(missing) == 0 {
		return nil, err
	}
	tx, err := t.Admin.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("%sbegin: %w", errPrefix, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // a rollback after a commit is a no-op; after a failure it is what keeps nothing
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "noema:key_rotated:"+t.ID); err != nil {
		return nil, fmt.Errorf("%slock the tenant's announcement: %w", errPrefix, err)
	}
	// Under the lock, a run that waited for another sees what that run committed.
	if missing, err = a.missing(ctx, t); err != nil || len(missing) == 0 {
		return nil, err
	}
	corr, err := a.Appender.IDs.ID("corr")
	if err != nil {
		return nil, fmt.Errorf("%scorrelation id: %w", errPrefix, err)
	}
	for _, id := range missing {
		ev := chain.Event{TenantID: t.ID, CorrelationID: corr, WrittenBy: Emitter, Actor: Emitter, EventType: EventType, ObjectRef: objectRef(id), Payload: map[string]any{"key_id": id}}
		if _, err := a.Appender.Append(ctx, tx, ev); err != nil {
			return nil, fmt.Errorf("%sappend %s for %s: %w", errPrefix, EventType, id, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%scommit: %w", errPrefix, err)
	}
	return missing, nil
}

// missing returns, in registry order, the key IDs that have no admin KEY_ROTATED on the tenant's chain.
func (a *Announcer) missing(ctx context.Context, t Tenant) ([]string, error) {
	keys, err := a.Keys.KeyIDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, nil
	}
	rows, err := chain.ReadChain(ctx, t.Reader)
	if err != nil {
		return nil, err
	}
	done := map[string]bool{}
	for _, r := range rows {
		if r.EventType != EventType || r.WrittenBy != Emitter || r.Payload == nil {
			continue
		}
		var p struct {
			KeyID string `json:"key_id"`
		}
		if json.Unmarshal([]byte(*r.Payload), &p) == nil && p.KeyID != "" {
			done[p.KeyID] = true
		}
	}
	var out []string
	for _, k := range keys {
		if !keyIDRe.MatchString(k) {
			return nil, fmt.Errorf("%sa registered key ID is not of the K1 shape", errPrefix)
		}
		if !done[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

// objectRef names the key object of the announcement: key_ and the first 32 hex digits of the SHA-256 of the key ID. It is a function of the key ID, so one key has one
// reference, and it is not the key ID spelled differently, so the append guard's check that an object_ref is minted by the core and never derived from a payload string
// (A184, record/payloadguard.DerivedObjectRef) holds.
func objectRef(keyID string) string {
	sum := sha256.Sum256([]byte(keyID))
	return "key_" + hex.EncodeToString(sum[:])[:32]
}
