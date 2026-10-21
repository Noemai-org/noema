// Package testpg gives tests a real PostgreSQL database of their own (REQ-TST-004: no in-memory stand-in).
// It reads the URL of a server whose user may create roles and databases from MOCK_CASE_TEST_DATABASE_URL, creates
// the two mock-case roles once, and hands each test a fresh database that is dropped afterwards.
// A missing variable fails the test; it never skips it. scripts/test.sh starts a throwaway server when needed.
package testpg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnvVar names the variable holding the server URL.
const EnvVar = "MOCK_CASE_TEST_DATABASE_URL"

const rolePassword = "test-only-not-a-secret" // SYNTHETIC: roles exist only on the throwaway test server

// DB is one test database.
type DB struct {
	AppDSN   string
	AdminDSN string
	Name     string
}

// New creates a database for t and registers its removal.
func New(t testing.TB) DB {
	t.Helper()
	base := os.Getenv(EnvVar)
	if base == "" {
		t.Fatalf("%s is not set: tests run against a real PostgreSQL. Run scripts/test.sh, or point the variable at a server", EnvVar)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to the test server: %v", err)
	}
	defer conn.Close(ctx)

	// Roles are cluster-wide and test packages run in parallel, so create them under an advisory lock.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(7402602)`); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, `
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mock_case_admin') THEN
    CREATE ROLE mock_case_admin LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '`+rolePassword+`';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mock_case_app') THEN
    CREATE ROLE mock_case_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD '`+rolePassword+`';
  END IF;
END $$`)
	conn.Exec(ctx, `SELECT pg_advisory_unlock(7402602)`) //nolint:errcheck
	if err != nil {
		t.Fatalf("create roles: %v", err)
	}

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "mc_test_" + hex.EncodeToString(suffix[:])
	if _, err := conn.Exec(ctx, `CREATE DATABASE `+name+` OWNER mock_case_admin`); err != nil {
		t.Fatalf("create database: %v", err)
	}
	// The public schema belongs to the database owner; the app role may use it but not create in it.
	t.Cleanup(func() {
		c2, cancel2 := context.WithTimeout(context.Background(), time.Minute)
		defer cancel2()
		cc, err := pgx.Connect(c2, base)
		if err != nil {
			return
		}
		defer cc.Close(c2)
		cc.Exec(c2, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`) //nolint:errcheck
	})
	return DB{Name: name, AppDSN: dsnFor(t, base, "mock_case_app", name), AdminDSN: dsnFor(t, base, "mock_case_admin", name)}
}

func dsnFor(t testing.TB, base, user, db string) string {
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("%s is not a URL: %v", EnvVar, err)
	}
	u.User = url.UserPassword(user, rolePassword)
	u.Path = "/" + db
	return u.String()
}
