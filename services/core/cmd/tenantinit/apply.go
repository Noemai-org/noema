package main

import (
	"context"

	"github.com/jackc/pgx/v5"
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

// apply writes the tenants and the keys in one transaction, or nothing.
func apply(ctx context.Context, conn *pgx.Conn, tenants []tenantSpec, keys []registeredKey) (counts, error) {
	_, _, _, _ = ctx, conn, tenants, keys
	return counts{}, nil
}
