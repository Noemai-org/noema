package keyannounce

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

// EventType is the Record event type of an announced key registration (PERD 32.2, control plane).
const EventType = "KEY_ROTATED"

// Emitter is the identity that writes and signs the announcement (DEC-2026-11-01 D2a).
const Emitter = "admin"

// KeyRegistry lists the key IDs of every signing_key row, whatever its status.
type KeyRegistry interface {
	KeyIDs(ctx context.Context) ([]string, error)
}

// PoolKeys is a KeyRegistry over a login that can SELECT from signing_key.
type PoolKeys struct{ Q chain.Queryer }

// KeyIDs returns the key IDs in (identity, version) order.
func (p PoolKeys) KeyIDs(ctx context.Context) ([]string, error) { return nil, nil }

// TxBeginner is a pool whose connections log in as one tenant's admin emitter login.
type TxBeginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Tenant is the connections the announcement of one tenant's chain needs.
type Tenant struct {
	ID     string
	Admin  TxBeginner    // the tenant's login bound to the admin emitter
	Reader chain.Queryer // the tenant's record_read login
}

// Announcer appends the announcements.
type Announcer struct {
	Appender *chain.Appender // its Signers hold the admin signer
	Keys     KeyRegistry
}

// Announce appends one KEY_ROTATED per key with none on the tenant's chain and returns their key IDs.
func (a *Announcer) Announce(ctx context.Context, t Tenant) ([]string, error) { return nil, nil }
