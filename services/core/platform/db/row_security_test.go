package db_test

// SEC-08 (REQ-SEC-004, INV-12): FAILING-FIRST tests for row security on the tenant-scoped tables. DEBT-04 binds the Record
// functions to the login's tenant; the other tenant tables (decision_grant, idempotency_record, outbox_event) are still read
// and written under the per-power group roles with whatever tenant_id the caller's SQL names. These tests connect as real
// per-tenant LOGIN roles (testpg.TenantLogin) and assert that a login for ten_a can neither see nor write a ten_b row.
// They are expected to FAIL on main until row security (policy on session_user via tenant_login, forced on the owner) lands.
// "Row security" restricts which rows a login sees; it does not authenticate a receipt (keep the two separate in reports).
// All data is SYNTHETIC.

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

type rowWorld struct {
	admin *pgxpool.Pool
	dsn   string
}

func newRowWorld(t *testing.T) *rowWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A'), ('ten_b', 'B')`); err != nil {
		t.Fatal(err)
	}
	exp := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tn := range []string{"a", "b"} {
		ten := "ten_" + tn
		if _, err := admin.Exec(ctx(t), `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
			VALUES ($1, $2, 'dec_'||$3::text, 'nonce_'||$3::text, '{}'::jsonb, 'pol', '1', $4, 'k1', 'sig')`, "grt_"+tn, ten, tn, exp); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx(t), `INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at) VALUES ($1, $2, 'topic', '{}'::jsonb, $3)`,
			"obx_"+tn, ten, exp); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx(t), `INSERT INTO idempotency_record (tenant_id, idempotency_key, logical_action, params_hash, attempt, status, last_grant_id, created_at, updated_at)
			VALUES ($1, 'idem_'||$2::text, 'act', 'h', 1, 'DISPATCHING', $3, $4, $4)`, ten, tn, "grt_"+tn, exp); err != nil {
			t.Fatal(err)
		}
	}
	return &rowWorld{admin: admin, dsn: dsn}
}

// Each power's login for ten_a sees no row of ten_b in any tenant table it can read, and still sees its own tenant's row.
func TestRowSecurityLoginSeesOnlyItsOwnTenantsRows(t *testing.T) {
	w := newRowWorld(t)
	reads := map[string][]string{ // group role -> tables that group may SELECT
		"execution_rw": {"decision_grant", "idempotency_record", "outbox_event"},
		"trust_rw":     {"decision_grant", "idempotency_record", "outbox_event"},
		"outcome_rw":   {"decision_grant", "idempotency_record", "outbox_event"},
	}
	for group, tables := range reads {
		pool := testpg.TenantLogin(t, w.admin, w.dsn, group, "ten_a")
		for _, table := range tables {
			var other, own int
			if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM `+table+` WHERE tenant_id = 'ten_b'`).Scan(&other); err != nil {
				t.Fatalf("%s reads %s: %v", group, table, err)
			}
			if other != 0 {
				t.Errorf("%s as ten_a sees %d row(s) of ten_b in %s, want 0", group, other, table)
			}
			if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM `+table+` WHERE tenant_id = 'ten_a'`).Scan(&own); err != nil {
				t.Fatalf("%s reads %s: %v", group, table, err)
			}
			if own != 1 {
				t.Errorf("%s as ten_a sees %d row(s) of its own tenant in %s, want 1", group, own, table)
			}
		}
	}
}

// A login for ten_a cannot write a row that names ten_b, in any table its power may insert into.
func TestRowSecurityLoginCannotWriteAnotherTenantsRow(t *testing.T) {
	w := newRowWorld(t)
	exp := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	trust := testpg.TenantLogin(t, w.admin, w.dsn, "trust_rw", "ten_a")
	if _, err := trust.Exec(ctx(t), `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		VALUES ('grt_x', 'ten_b', 'dec_x', 'nonce_x', '{}'::jsonb, 'pol', '1', $1, 'k1', 'sig')`, exp); err == nil {
		t.Error("trust_rw as ten_a inserted a decision_grant for ten_b")
	}
	for _, group := range []string{"trust_rw", "execution_rw", "outcome_rw"} {
		pool := testpg.TenantLogin(t, w.admin, w.dsn, group, "ten_a")
		if _, err := pool.Exec(ctx(t), `INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at) VALUES ('obx_x_'||$2::text, 'ten_b', 'topic', '{}'::jsonb, $1)`, exp, group); err == nil {
			t.Errorf("%s as ten_a inserted an outbox_event for ten_b", group)
		}
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM decision_grant WHERE grant_id = 'grt_x'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("a cross-tenant decision_grant was stored: %d row(s)", n)
	}
}

// A login for ten_a cannot change the state of a ten_b row: the void or spend of another tenant's grant, or its dispatch mark.
func TestRowSecurityLoginCannotUpdateAnotherTenantsRow(t *testing.T) {
	w := newRowWorld(t)
	exec := testpg.TenantLogin(t, w.admin, w.dsn, "execution_rw", "ten_a")
	tag, err := exec.Exec(ctx(t), `UPDATE decision_grant SET status = 'VOID' WHERE grant_id = 'grt_b'`)
	if err == nil && tag.RowsAffected() != 0 {
		t.Errorf("execution_rw as ten_a changed %d decision_grant row(s) of ten_b", tag.RowsAffected())
	}
	tag, err = exec.Exec(ctx(t), `UPDATE idempotency_record SET status = 'RECEIPTED' WHERE tenant_id = 'ten_b'`)
	if err == nil && tag.RowsAffected() != 0 {
		t.Errorf("execution_rw as ten_a changed %d idempotency_record row(s) of ten_b", tag.RowsAffected())
	}
	orch := testpg.TenantLogin(t, w.admin, w.dsn, "orchestrator_rw", "ten_a")
	tag, err = orch.Exec(ctx(t), `UPDATE outbox_event SET dispatched_at = now() WHERE tenant_id = 'ten_b'`)
	if err == nil && tag.RowsAffected() != 0 {
		t.Errorf("orchestrator_rw as ten_a marked %d outbox_event row(s) of ten_b dispatched", tag.RowsAffected())
	}
	var status string
	if err := w.admin.QueryRow(ctx(t), `SELECT status FROM decision_grant WHERE grant_id = 'grt_b'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "ISSUED" {
		t.Errorf("ten_b's grant is %s after ten_a's login tried to void it, want ISSUED", status)
	}
}

// Row security must hold on the table owner too: a table that is not FORCEd lets its owner role read across tenants.
func TestRowSecurityIsEnabledAndForcedOnEveryTenantTable(t *testing.T) {
	w := newRowWorld(t)
	rows, err := w.admin.Query(ctx(t), `SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		  AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)
		  AND c.relname NOT IN ('record_event', 'record_chain_head', 'tenant_login')
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var name string
		var enabled, forced bool
		if err := rows.Scan(&name, &enabled, &forced); err != nil {
			t.Fatal(err)
		}
		n++
		if !enabled || !forced {
			t.Errorf("table %s has a tenant_id column but row security enabled=%v forced=%v", name, enabled, forced)
		}
	}
	if n == 0 {
		t.Fatal("found no tenant-scoped table: the query is wrong")
	}
}
