package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// apply.go is the work. The database decides what is allowed: the tenant table forces row-level security with no INSERT policy (migration 0030) and key_register is executable by its
// owner and the superuser only (migration 0013), so a login that may not do this is refused by the database, not by a check here. This file adds the idempotent answers a
// seeding step needs: an existing tenant with the same name and a key already registered with the same public key are "existing"; the same ID with another name, or the same
// version with another public key, is a refusal. Everything is written in one transaction, so a refusal keeps nothing.

const keyIdentity = "core-trust"

// The sentinels carry the cause of a refusal for errors.Is. None of their texts is printed (A347): failure() chooses the public sentence.
var (
	errNameDiffers  = errors.New("tenantinit: a tenant exists under another name")
	errKeyDiffers   = errors.New("tenantinit: a different public key is registered under this version")
	errKeyNotActive = errors.New("tenantinit: the registered key of this version is not active")
	errKeyRefused   = errors.New("tenantinit: the database refused to register the key")
	errLoginDenied  = errors.New("tenantinit: the login may not do this")
)

// registeredKey is the public half of one core-trust signing key, by version. It never holds private bytes.
type registeredKey struct {
	version int
	public  []byte
}

// counts is the one line of output: how many rows were written and how many were already as wanted.
type counts struct {
	TenantsCreated  int `json:"tenants_created"`
	TenantsExisting int `json:"tenants_existing"`
	KeysRegistered  int `json:"keys_registered"`
	KeysExisting    int `json:"keys_existing"`
}

const (
	sqlStateInsufficientPrivilege = "42501"
	sqlStateIntegrityClass        = "23"
)

// dbError keeps the cause of a database error and, for a login the database refuses, says so with errLoginDenied.
func dbError(step string, err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == sqlStateInsufficientPrivilege {
		return fmt.Errorf("%w: %s: %w", errLoginDenied, step, err)
	}
	return fmt.Errorf("tenantinit: %s: %w", step, err)
}

// apply writes the tenants and then the keys (in version order) in one transaction, or nothing. keys are sorted by ascending version.
func apply(ctx context.Context, conn *pgx.Conn, tenants []tenantSpec, keys []registeredKey) (counts, error) {
	var c counts
	tx, err := conn.Begin(ctx)
	if err != nil {
		return c, dbError("begin", err)
	}
	// A transaction that was committed ignores this; one that was not is ended, so a refusal keeps nothing.
	defer func() { _ = tx.Rollback(ctx) }()
	for _, t := range tenants {
		created, err := ensureTenant(ctx, tx, t)
		if err != nil {
			return counts{}, err
		}
		if created {
			c.TenantsCreated++
		} else {
			c.TenantsExisting++
		}
	}
	for _, k := range keys {
		registered, err := ensureKey(ctx, tx, k)
		if err != nil {
			return counts{}, err
		}
		if registered {
			c.KeysRegistered++
		} else {
			c.KeysExisting++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return counts{}, dbError("commit", err)
	}
	return c, nil
}

// ensureTenant inserts the row when it is absent and never rewrites one: an existing row with another name is a refusal, and its kill switch and time zone are not touched.
func ensureTenant(ctx context.Context, tx pgx.Tx, t tenantSpec) (created bool, err error) {
	tag, err := tx.Exec(ctx, `INSERT INTO tenant (tenant_id, name) VALUES ($1, $2) ON CONFLICT (tenant_id) DO NOTHING`, t.id, t.name)
	if err != nil {
		return false, dbError("insert a tenant", err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	var name string
	if err := tx.QueryRow(ctx, `SELECT name FROM tenant WHERE tenant_id = $1`, t.id).Scan(&name); err != nil {
		return false, dbError("read a tenant", err)
	}
	if name != t.name {
		return false, errNameDiffers
	}
	return false, nil
}

// ensureKey registers the public key of a version through key_register, the only way a key enters signing_key, or accepts the registration that is already there when it holds the same
// public key and is active. The key ID is derived by key_register from the identity and the version, never supplied.
func ensureKey(ctx context.Context, tx pgx.Tx, k registeredKey) (registered bool, err error) {
	var stored []byte
	var status string
	err = tx.QueryRow(ctx, `SELECT public_key, status FROM signing_key WHERE identity = $1 AND version = $2`, keyIdentity, int32(k.version)).Scan(&stored, &status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := tx.Exec(ctx, `SELECT public.key_register($1, $2, $3)`, keyIdentity, int32(k.version), k.public); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && strings.HasPrefix(pg.Code, sqlStateIntegrityClass) {
				return false, fmt.Errorf("%w: %w", errKeyRefused, err)
			}
			return false, dbError("register a key", err)
		}
		return true, nil
	case err != nil:
		return false, dbError("read a key", err)
	case !bytes.Equal(stored, k.public):
		return false, errKeyDiffers
	case status != "ACTIVE":
		return false, errKeyNotActive
	}
	return false, nil
}
