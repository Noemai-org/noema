package db_test

import (
	"io/fs"
	"reflect"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

const readChain = `SELECT count(*) FROM record_read_chain_t()`

// REQ-AUD-003, REQ-AUD-005 (REC-03, approved escalation E1): record_read reaches the Record through
// record_read_chain and nothing else. It cannot read or write the tables, and it cannot append.
func TestRecordReadRoleReadsOnlyThroughTheReadFunction(t *testing.T) {
	pool, dsn := migratedDSN(t)
	seed(t, pool)
	login := testpg.TenantLogin(t, pool, dsn, "record_read", "ten_a")
	if _, err := login.Exec(ctx(t), readChain); err != nil {
		t.Fatalf("a record_read login calling record_read_chain_t: %v", err)
	}
	for _, stmt := range []string{
		`SELECT * FROM record_event`,
		`SELECT * FROM record_chain_head`,
		`INSERT INTO record_event (event_id, tenant_id, seq, prev_hash, hash, written_by, event_type, object_ref, payload_hash, correlation_id, at, schema_version, actor, body)
		 VALUES ('recev_x', 'ten_a', 1, 'p', 'h', 'w', 'T', 'o', 'ph', 'c', now(), '1', 'a', '{}')`,
		`UPDATE record_chain_head SET seq = 99`,
		`SELECT * FROM record_lock_head_t()`,
		`SELECT * FROM decision_grant`,
		`SELECT * FROM execution_receipt`,
	} {
		wantCode(t, try(t, pool, "record_read", stmt), denied, "record_read: "+stmt)
	}
}

// REQ-AUD-001 (REC-03, E1): EXECUTE on record_read_chain is revoked from PUBLIC and granted only to record_read.
// Every power role, and a role created after the migration (which holds only what PUBLIC holds), is refused.
// record_append is not in the list: it owns the Record tables and the function, and no login maps to it.
func TestEveryOtherRoleIsRefusedTheReadFunction(t *testing.T) {
	pool := migrated(t)
	seed(t, pool)
	if _, err := pool.Exec(ctx(t), `CREATE ROLE some_later_role NOLOGIN`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx(t), `DROP ROLE IF EXISTS some_later_role`) })
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "some_later_role"} {
		wantCode(t, try(t, pool, role, readChain), denied, role+" calling record_read_chain")
	}
}

// DEBT-04 (migration 0005): no Record function takes its tenant from the caller.
func TestNoRecordFunctionTakesATenantFromTheCaller(t *testing.T) {
	pool := migrated(t)
	var n int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM pg_proc WHERE proname IN ('record_lock_head', 'record_insert', 'record_read_chain')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d Record functions with a tenant argument remain", n)
	}
}

func migrationsWith(t *testing.T, extra map[string]*fstest.MapFile, skip string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n == skip {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, n)
		if err != nil {
			t.Fatal(err)
		}
		out[n] = &fstest.MapFile{Data: body}
	}
	for n, f := range extra {
		out[n] = f
	}
	return out
}

// privileges is every ACL in the public schema: functions and tables.
func privileges(t *testing.T, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx(t), `
		SELECT 'fn:' || p.oid::regprocedure::text, coalesce(p.proacl::text, '') FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace
		UNION ALL
		SELECT 'rel:' || c.relname, coalesce(c.relacl::text, '') FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'S', 'v')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// REC-03 (E1): Migrate has no gap check. A database migrated without the grant-void migration (0014, noema#94) takes it later; a
// fresh database takes everything in order. Both end with the same privileges, and record_read works in both. It uses the real file:
// 0014 replaces decision_grant_guard and must not disturb a Record privilege.
func TestTheGrantVoidMigrationAppliedLateGivesTheSamePrivilegesAsAFreshDatabase(t *testing.T) {
	const late = "0014_grant_spent_void.sql"
	fresh, _ := testpg.NewDatabase(t)
	all := migrationsWith(t, nil, "")
	if got, err := db.Migrate(ctx(t), fresh, all); err != nil || !slices.Contains(got, late) {
		t.Fatalf("fresh migrate: applied %v, err %v; want %s among them", got, err, late)
	}

	upgraded, _ := testpg.NewDatabase(t)
	if got, err := db.Migrate(ctx(t), upgraded, migrationsWith(t, nil, late)); err != nil || !slices.Contains(got, "0003_record_read.sql") || slices.Contains(got, late) {
		t.Fatalf("first migrate without %s: applied %v, err %v", late, got, err)
	}
	if got, err := db.Migrate(ctx(t), upgraded, all); err != nil || !reflect.DeepEqual(got, []string{late}) {
		t.Fatalf("upgrade: applied %v, err %v; want only the late %s", got, err, late)
	}

	if a, b := privileges(t, fresh), privileges(t, upgraded); !reflect.DeepEqual(a, b) {
		t.Fatalf("privileges differ between the two orders:\nfresh:    %v\nupgraded: %v", a, b)
	}
	for _, pool := range []*pgxpool.Pool{fresh, upgraded} {
		var reads, writerReads bool
		if err := pool.QueryRow(ctx(t), `SELECT has_function_privilege('record_read', 'record_read_chain_t()', 'EXECUTE'), has_function_privilege('execution_rw', 'record_read_chain_t()', 'EXECUTE')`).Scan(&reads, &writerReads); err != nil {
			t.Fatal(err)
		}
		if !reads || writerReads {
			t.Fatalf("after both orders record_read may execute the read function: %v, execution_rw may: %v", reads, writerReads)
		}
	}
}
