package db_test

// Ruling A6 (6 Nov 2026), hardening of the Record functions (DEBT-04 follow-up; REQ-SEC-004, INV-12, INV-13, REQ-AUD-001): every SECURITY DEFINER
// function owned by record_append pins search_path to pg_catalog and pg_temp, in that order with nothing else, and names every table by its
// schema. With `pg_catalog, public` the session's temporary schema is searched first for a table, so a login with the TEMPORARY privilege could
// create pg_temp.tenant_login and make record_login_tenant read a mapping it chose. All data is SYNTHETIC.

import (
	"reflect"
	"testing"
)

func TestEveryRecordFunctionPinsItsSearchPathToCatalogAndTemp(t *testing.T) {
	pool := migrated(t)
	rows, err := pool.Query(ctx(t), `SELECT p.oid::regprocedure::text, p.prosecdef, coalesce(p.proconfig, '{}')
		FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace AND p.prokind = 'f' AND pg_get_userbyid(p.proowner) = 'record_append' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var sig string
		var definer bool
		var cfg []string
		if err := rows.Scan(&sig, &definer, &cfg); err != nil {
			t.Fatal(err)
		}
		seen++
		if !definer {
			t.Errorf("%s is owned by record_append and is not SECURITY DEFINER", sig)
		}
		if want := []string{"search_path=pg_catalog, pg_temp"}; !reflect.DeepEqual(cfg, want) {
			t.Errorf("%s: proconfig = %v, want %v", sig, cfg, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen < 5 {
		t.Fatalf("only %d functions owned by record_append: the catalog query lost them", seen)
	}
}

// The exploit the pin closes: a writer login makes a temporary table named tenant_login that maps itself to another tenant, then calls the
// Record writer. The function must neither use the table nor touch the other tenant's chain.
func TestATemporaryTenantLoginTableDoesNotRedirectARecordCall(t *testing.T) {
	w := newTenantWorld(t)
	login := w.login(t, w.name("execution_rw", "ten_a"))
	conn, err := login.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx(t), `CREATE TEMP TABLE tenant_login (login_role text, power text, tenant_id text, emitter text)`); err != nil {
		t.Skipf("this login cannot create temporary tables, nothing to shadow with: %v", err)
	}
	if _, err := conn.Exec(ctx(t), `INSERT INTO tenant_login VALUES (session_user, 'execution', 'ten_b', NULL)`); err != nil {
		t.Fatal(err)
	}
	// The definer runs as record_append, which has no right on the attacker's table; the attacker grants it (the table is the attacker's own).
	if _, err := conn.Exec(ctx(t), `GRANT SELECT ON tenant_login TO record_append`); err != nil {
		t.Fatal(err)
	}
	var seq int64
	var hash string
	if err := conn.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &hash); err != nil {
		t.Logf("record_lock_head_t: %v", err)
	}
	var other int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM record_chain_head WHERE tenant_id = 'ten_b'`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if other != 0 {
		t.Fatalf("a temporary tenant_login redirected the call: a chain head of ten_b was created")
	}
	var own int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM record_chain_head WHERE tenant_id = 'ten_a'`).Scan(&own); err != nil || own != 1 {
		t.Fatalf("the login's own chain head: count %d, err %v", own, err)
	}
}

// SR design review of noema#234 (7 Nov 2026), ruling A6: the three definer functions of migration 0007 (Outcome's verdict path) read execution_receipt,
// decision_grant and the verdict tables, and still searched `pg_catalog, public`: a login with TEMPORARY could shadow those tables with its own.
// They pin `pg_catalog, pg_temp` and name every table by its schema, like the five above. (SEC-08, noema#251, checks every definer function.)
func TestTheOutcomeDefinerFunctionsPinTheirSearchPathToo(t *testing.T) {
	pool := migrated(t)
	for _, fn := range []string{"outcome_record_verdict", "outcome_verdict_complete", "recovery_request_verdict_guard"} {
		var cfg []string
		var definer bool
		if err := pool.QueryRow(ctx(t), `SELECT p.prosecdef, coalesce(p.proconfig, '{}') FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace AND p.proname = $1`, fn).Scan(&definer, &cfg); err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		if !definer {
			t.Errorf("%s is not SECURITY DEFINER", fn)
		}
		if want := []string{"search_path=pg_catalog, pg_temp"}; !reflect.DeepEqual(cfg, want) {
			t.Errorf("%s: proconfig = %v, want %v", fn, cfg, want)
		}
	}
}

// outcome_record_verdict calls record_login_tenant and, when the migration role creates it (production: noema_migrator owns it; in the test cluster the administrator does,
// so its own privileges hide this), it runs as noema_migrator, which needs EXECUTE on record_login_tenant. Removing the grant of migration 0016 went unseen
// (definition of done 3c, A131), so the grant is pinned in the catalog, with the other roles that must not hold it.
func TestTheMigrationRoleCanExecuteRecordLoginTenantAndPublicCannot(t *testing.T) {
	pool := migrated(t)
	var can bool
	if err := pool.QueryRow(ctx(t), `SELECT has_function_privilege('noema_migrator', 'public.record_login_tenant(text)', 'EXECUTE')`).Scan(&can); err != nil {
		t.Fatal(err)
	}
	if !can {
		t.Fatal("noema_migrator cannot execute record_login_tenant: outcome_record_verdict, owned by the migration role, would fail for every verdict (0016)")
	}
	var public bool
	if err := pool.QueryRow(ctx(t), `SELECT has_function_privilege('public', 'public.record_login_tenant(text)', 'EXECUTE')`).Scan(&public); err != nil {
		t.Fatal(err)
	}
	if public {
		t.Fatal("PUBLIC can execute record_login_tenant")
	}
}
