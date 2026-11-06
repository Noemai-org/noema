package db_test

// EXE-08 (W4) slice A, DEBT-05 (DEC-2026-11-01-receipt-and-record-signing D14, D15, D16, section 6.1; test 7 and the pins): the Record
// tables belong to the migration role, no runtime role can change them, and no runtime role has CREATE anywhere.

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runtimeRoles are every role a running service can act as.
var runtimeRoles = []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "record_append", "record_read", "record_verify", "record_emitter"}

// D15: the Record tables are owned by noema_migrator, not by the role whose definer functions append to them.
func TestTheRecordTablesAreOwnedByTheMigrationRole(t *testing.T) {
	pool := migrated(t)
	for _, tbl := range []string{"record_event", "record_chain_head"} {
		var owner string
		if err := pool.QueryRow(ctx(t), `SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid = $1::regclass`, tbl).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		if owner != "noema_migrator" {
			t.Errorf("%s is owned by %s, want noema_migrator", tbl, owner)
		}
	}
}

// D16 / DEBT-05 / test 7: no runtime role holds CREATE on any schema (so none can shadow a name through search_path), on the database,
// or TEMPORARY on the database.
func TestNoRuntimeRoleHoldsCreateAnywhere(t *testing.T) {
	pool := migrated(t)
	for _, role := range runtimeRoles {
		rows, err := pool.Query(ctx(t), `SELECT nspname FROM pg_namespace WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema' AND has_schema_privilege($1, oid, 'CREATE')`, role)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var ns string
			_ = rows.Scan(&ns)
			t.Errorf("%s holds CREATE on schema %s", role, ns)
		}
		rows.Close()
		var dbCreate, dbTemp bool
		if err := pool.QueryRow(ctx(t), `SELECT has_database_privilege($1, current_database(), 'CREATE'), has_database_privilege($1, current_database(), 'TEMPORARY')`, role).Scan(&dbCreate, &dbTemp); err != nil {
			t.Fatal(err)
		}
		if dbCreate {
			t.Errorf("%s holds CREATE on the database", role)
		}
	}
}

// D14: no runtime role is a member of noema_migrator, directly or through another role; none has a privileged attribute.
func TestNoRuntimeRoleReachesTheMigrationRoleOrHoldsAPrivilegedAttribute(t *testing.T) {
	pool := migrated(t)
	for _, role := range runtimeRoles {
		var member, super, createRole, createDB, repl, bypass bool
		if err := pool.QueryRow(ctx(t), `SELECT pg_has_role($1, 'noema_migrator', 'MEMBER'), r.rolsuper, r.rolcreaterole, r.rolcreatedb, r.rolreplication, r.rolbypassrls
			FROM pg_roles r WHERE r.rolname = $1`, role).Scan(&member, &super, &createRole, &createDB, &repl, &bypass); err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if member || super || createRole || createDB || repl || bypass {
			t.Errorf("%s: member of noema_migrator %v, superuser %v, createrole %v, createdb %v, replication %v, bypassrls %v", role, member, super, createRole, createDB, repl, bypass)
		}
	}
}

// D16: record_append is the definer of the append functions and holds the grants they need, but it cannot alter, drop, truncate,
// disable a trigger on, or replace anything about the Record tables or their functions' targets.
func TestRecordAppendCannotChangeTheRecordTables(t *testing.T) {
	pool, dsn := migratedDSN(t)
	login := recordAppendLogin(t, pool, dsn)
	for _, stmt := range []string{
		`ALTER TABLE record_event DISABLE TRIGGER ALL`,
		`ALTER TABLE record_event ENABLE REPLICA TRIGGER record_event_immutable`,
		`ALTER TABLE record_event ADD COLUMN x int`,
		`ALTER TABLE record_chain_head ADD COLUMN x int`,
		`ALTER TABLE record_event OWNER TO record_append`,
		`DROP TABLE record_event`,
		`DROP TABLE record_chain_head`,
		`TRUNCATE record_event`,
		`TRUNCATE record_chain_head`,
		`DELETE FROM record_event`,
		`UPDATE record_event SET event_type = event_type`,
		`DROP TRIGGER record_event_immutable ON record_event`,
		`CREATE TABLE shadow (i int)`,
		`CREATE FUNCTION public.shadow_fn() RETURNS int LANGUAGE sql AS 'SELECT 1'`,
		`CREATE SCHEMA shadow`,
	} {
		_, err := login.Exec(ctx(t), stmt)
		wantCode(t, err, denied, "record_append: "+stmt)
	}
}

// recordAppendLogin is a throwaway login that is a member of record_append, to try what the role may do.
func recordAppendLogin(t *testing.T, admin *pgxpool.Pool, dsn string) *pgxpool.Pool {
	t.Helper()
	w := &tenantWorld{admin: admin, dsn: dsn}
	role := "append_probe_" + w.suffixOf(t)
	if _, err := admin.Exec(ctx(t), `CREATE ROLE `+pgx.Identifier{role}.Sanitize()+` LOGIN IN ROLE record_append`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx(t), `DROP OWNED BY `+pgx.Identifier{role}.Sanitize()+`; DROP ROLE IF EXISTS `+pgx.Identifier{role}.Sanitize())
	})
	return w.login(t, role)
}
