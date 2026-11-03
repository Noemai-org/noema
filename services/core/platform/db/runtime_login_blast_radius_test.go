package db_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

// SEC-13 (W1 closure check), TM-26, TM-10, REQ-DATA-001, REQ-AUD-001, INV-29. The core's runtime database login must not be
// able to destroy or rebuild the schema that holds the Record, the receipts and the grants.
//
// Compose today creates the core's login, noema_core, as the OWNER of the database (deploy/compose/postgres/init.sh:
// CREATE DATABASE noema OWNER noema_core). In PostgreSQL 15 and later the owner of the database is a member of pg_database_owner,
// which owns schema public, and a schema owner can DROP SCHEMA ... CASCADE: it drops every table, trigger and function in it, and
// can then create the schema again with content of its own. That is a wholesale rewrite of the Record by a runtime credential,
// with no per-table privilege involved. Checked on 3 Nov 2026 on PostgreSQL 16 with migration 0001 applied as in Compose.
//
// This is the "migrator credential absent from the core's runtime" check of the W1 design (DEC-2026-11-01 D14 to D16, "no runtime
// role has CREATE on any schema"), at the database-owner level that D14 does not name. It FAILS on today's setup and passes when the
// runtime login owns nothing and has no CREATE on the database or any schema.
//
// The test builds the Compose situation exactly: a login role that owns the database, the migrations applied by the superuser.

func ownerLogin(t *testing.T, name string) *pgx.Conn {
	t.Helper()
	pool := migrated(t)
	var dbname string
	if err := pool.QueryRow(ctx(t), `SELECT current_database()`).Scan(&dbname); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE ROLE ` + name + ` LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION`,
		`ALTER DATABASE ` + pgx.Identifier{dbname}.Sanitize() + ` OWNER TO ` + name, // what init.sh does for noema_core
	} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cfg := pool.Config().ConnConfig.Copy()
	cfg.User = name
	cfg.Password = ""
	conn, err := pgx.ConnectConfig(ctx(t), cfg)
	if err != nil {
		t.Fatalf("connect as %s: %v", name, err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx(t)) })
	return conn
}

// The destructive statements are run inside a transaction that is always rolled back.
func refusedInTx(t *testing.T, conn *pgx.Conn, stmt string) error {
	t.Helper()
	tx, err := conn.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	_, err = tx.Exec(ctx(t), stmt)
	return err
}

func TestTheRuntimeLoginCannotDropOrRebuildTheSchema(t *testing.T) {
	conn := ownerLogin(t, "noema_core_like")
	for name, stmt := range map[string]string{
		"DROP SCHEMA public CASCADE (drops the Record, the receipts, the grants and the triggers)": `DROP SCHEMA public CASCADE`,
		"CREATE SCHEMA (a schema of its own to rebuild into)":                                      `CREATE SCHEMA rebuilt`,
		"ALTER SCHEMA public OWNER TO itself, then everything under it is its to change":           `ALTER SCHEMA public OWNER TO noema_core_like`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := refusedInTx(t, conn, stmt); err == nil {
				t.Fatalf("SEC-13 FINDING: the runtime login %q executed %q; it must own nothing and hold no CREATE on the database or a schema", "noema_core_like", stmt)
			}
		})
	}
	var member bool
	if err := conn.QueryRow(ctx(t), `SELECT pg_has_role(current_user, 'pg_database_owner', 'member')`).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if member {
		t.Fatalf("SEC-13 FINDING: the runtime login is a member of pg_database_owner, which owns schema public")
	}
}

// Control: a login that owns nothing and holds only CONNECT and USAGE on public is refused every one of those statements. It shows the
// harness can pass, so the failure above is the ownership and not the test.
func TestAControlLoginThatOwnsNothingIsRefused(t *testing.T) {
	pool := migrated(t)
	var dbname string
	if err := pool.QueryRow(ctx(t), `SELECT current_database()`).Scan(&dbname); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE ROLE control_login LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION`,
		`GRANT CONNECT ON DATABASE ` + pgx.Identifier{dbname}.Sanitize() + ` TO control_login`,
		`GRANT USAGE ON SCHEMA public TO control_login`,
	} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cfg := pool.Config().ConnConfig.Copy()
	cfg.User, cfg.Password = "control_login", ""
	conn, err := pgx.ConnectConfig(ctx(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx(t)) }()
	for _, stmt := range []string{`DROP SCHEMA public CASCADE`, `CREATE SCHEMA rebuilt`, `ALTER SCHEMA public OWNER TO control_login`} {
		if err := refusedInTx(t, conn, stmt); err == nil {
			t.Fatalf("the control login executed %q", stmt)
		}
	}
}
