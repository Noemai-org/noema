package db_test

// SEC-08 (REQ-SEC-004, INV-12): FAILING-FIRST tests for row security on the tenant-scoped tables. DEBT-04 binds the Record
// functions to the login's tenant; the other tenant tables (decision_grant, idempotency_record, outbox_event) are still read
// and written under the per-power group roles with whatever tenant_id the caller's SQL names. These tests connect as real
// per-tenant LOGIN roles (testpg.TenantLogin) and assert that a login for ten_a can neither see nor write a ten_b row.
// They are expected to FAIL on main until row security (policy on session_user via tenant_login, forced on the owner) lands.
// "Row security" restricts which rows a login sees; it does not authenticate a receipt (keep the two separate in reports).
// All data is SYNTHETIC.

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
		// The grant guard (noema#168) needs an ALLOW record of the same tenant first, and a grant that agrees with it (intent hash, policy version, window).
		if _, err := admin.Exec(ctx(t), `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
			VALUES ('dec_'||$1::text, $2, jsonb_build_object('decision_id', 'dec_'||$1::text, 'tenant_id', $2::text, 'outcome', 'ALLOW', 'sig', jsonb_build_object('key_id', 'k1', 'value', 'aa')) || `+testpg.BoundFieldsSQL+`, 'ALLOW', '`+testpg.FixtureIntentHash+`', '[]', 'k1', 'aa')`, tn, ten); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx(t), `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
			VALUES ($1, $2, 'dec_'||$3::text, 'nonce_'||$3::text, jsonb_build_object('decision_id', 'dec_'||$3::text, 'tenant_id', $2::text) || `+testpg.GrantBoundSQL+`, 'pol_1', '1', $4, 'k1', 'sig')`, "grt_"+tn, ten, tn, exp); err != nil {
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
	// The grant guard (noema#168) needs an ALLOW record of the grant's own tenant, so the only thing left to refuse the cross-tenant insert is row
	// security, not a missing record (a refusal for the wrong reason would prove nothing). Both records are made by the admin.
	for _, rec := range [][2]string{{"dec_x", "ten_b"}, {"dec_own", "ten_a"}} {
		if _, err := w.admin.Exec(ctx(t), `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
			VALUES ($1, $2, jsonb_build_object('decision_id', $1::text, 'tenant_id', $2::text, 'outcome', 'ALLOW', 'sig', jsonb_build_object('key_id', 'k1', 'value', 'aa')) || `+testpg.BoundFieldsSQL+`, 'ALLOW', '`+testpg.FixtureIntentHash+`', '[]', 'k1', 'aa')`, rec[0], rec[1]); err != nil {
			t.Fatal(err)
		}
	}
	const insertGrant = `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		VALUES ($1, $2, $3, 'nonce_'||$1::text, jsonb_build_object('decision_id', $3::text, 'tenant_id', $2::text) || ` + testpg.GrantBoundSQL + `, 'pol_1', '1', $4, 'k1', 'sig')`
	if _, err := trust.Exec(ctx(t), insertGrant, "grt_x", "ten_b", "dec_x", exp); err == nil {
		t.Error("trust_rw as ten_a inserted a decision_grant for ten_b")
	}
	// Positive control: the same statement for the login's OWN tenant succeeds, so a refusal above can only be about the tenant.
	if _, err := trust.Exec(ctx(t), insertGrant, "grt_own", "ten_a", "dec_own", exp); err != nil {
		t.Fatalf("control: trust_rw as ten_a cannot insert its own tenant's decision_grant: %v", err)
	}
	pools := map[string]*pgxpool.Pool{
		"trust_rw":     trust, // one login per role and tenant: TenantLogin must not be called twice for the same pair
		"execution_rw": testpg.TenantLogin(t, w.admin, w.dsn, "execution_rw", "ten_a"),
		"outcome_rw":   testpg.TenantLogin(t, w.admin, w.dsn, "outcome_rw", "ten_a"),
	}
	for group, pool := range pools {
		if _, err := pool.Exec(ctx(t), `INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at) VALUES ('obx_own_'||$2::text, 'ten_a', 'topic', '{}'::jsonb, $1)`, exp, group); err != nil {
			t.Fatalf("control: %s as ten_a cannot insert its own tenant's outbox_event: %v", group, err)
		}
		if _, err := pool.Exec(ctx(t), `INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at) VALUES ('obx_x_'||$2::text, 'ten_b', 'topic', '{}'::jsonb, $1)`, exp, group); err == nil {
			t.Errorf("%s as ten_a inserted an outbox_event for ten_b", group)
		}
	}
	// Codex P2 on #190: idempotency_record is an INSERT path of execution_rw as well. A same-tenant control proves the statement works,
	// so the cross-tenant refusal can only be about the tenant.
	const insertIdem = `INSERT INTO idempotency_record (tenant_id, idempotency_key, logical_action, params_hash, attempt, status, last_grant_id, created_at, updated_at)
		VALUES ($1, $2, 'act', 'h', 1, 'DISPATCHING', $3, $4, $4)`
	if _, err := pools["execution_rw"].Exec(ctx(t), insertIdem, "ten_a", "idem_own", "grt_a", exp); err != nil {
		t.Fatalf("control: execution_rw as ten_a cannot insert its own tenant's idempotency_record: %v", err)
	}
	if _, err := pools["execution_rw"].Exec(ctx(t), insertIdem, "ten_b", "idem_x", "grt_b", exp); err == nil {
		t.Error("execution_rw as ten_a inserted an idempotency_record for ten_b")
	}
	var idemRows int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM idempotency_record WHERE idempotency_key = 'idem_x'`).Scan(&idemRows); err != nil {
		t.Fatal(err)
	}
	if idemRows != 0 {
		t.Errorf("a cross-tenant idempotency_record was stored: %d row(s)", idemRows)
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
	// Positive control: the same statements against ten_a's own rows change exactly one row each.
	for _, q := range []string{`UPDATE decision_grant SET status = 'VOID' WHERE grant_id = 'grt_a'`, `UPDATE idempotency_record SET status = 'RECEIPTED' WHERE tenant_id = 'ten_a'`} {
		if tag, err := exec.Exec(ctx(t), q); err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("control: %q on ten_a's own row: rows=%d err=%v, want 1 row", q, tag.RowsAffected(), err)
		}
	}
	tag, err := exec.Exec(ctx(t), `UPDATE decision_grant SET status = 'VOID' WHERE grant_id = 'grt_b'`)
	if err == nil && tag.RowsAffected() != 0 {
		t.Errorf("execution_rw as ten_a changed %d decision_grant row(s) of ten_b", tag.RowsAffected())
	}
	tag, err = exec.Exec(ctx(t), `UPDATE idempotency_record SET status = 'RECEIPTED' WHERE tenant_id = 'ten_b'`)
	if err == nil && tag.RowsAffected() != 0 {
		t.Errorf("execution_rw as ten_a changed %d idempotency_record row(s) of ten_b", tag.RowsAffected())
	}
	orch := testpg.TenantLogin(t, w.admin, w.dsn, "orchestrator_rw", "ten_a")
	if tag, err := orch.Exec(ctx(t), `UPDATE outbox_event SET dispatched_at = now() WHERE tenant_id = 'ten_a'`); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("control: orchestrator_rw on ten_a's own outbox row: rows=%d err=%v, want 1 row", tag.RowsAffected(), err)
	}
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

// Row security must hold on the table owner too: a table that is not FORCEd lets its owner role read across tenants. Ruling A6 puts
// record_event and record_chain_head in the list (the Record functions are owned by record_append, so FORCE is what makes the policy apply to
// them); only the tenant catalog and the login map are left out, because every login must read them.
func TestRowSecurityIsEnabledAndForcedOnEveryTenantTable(t *testing.T) {
	w := newRowWorld(t)
	rows, err := w.admin.Query(ctx(t), `SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		  AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)
		  AND c.relname NOT IN ('tenant', 'tenant_login')
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

// Positive control for ruling A6 (record_event and record_chain_head get row security with FORCE): the chain verifier (REC-05) reads every
// tenant's Record by design, with no tenant mapping, and must still do so once the policy exists. It reads, it does not write. Passes today;
// it keeps the all-tenants policy from being forgotten when RLS is added.
func TestRowSecurityTheVerifierStillReadsEveryTenantsChainHeadAndWritesNothing(t *testing.T) {
	w := newRowWorld(t)
	for _, ten := range []string{"ten_a", "ten_b"} {
		if _, err := w.admin.Exec(ctx(t), `INSERT INTO record_chain_head (tenant_id, seq, hash) VALUES ($1, 0, 'sha256:' || repeat('0', 64))`, ten); err != nil {
			t.Fatal(err)
		}
	}
	pool := w.verifierPool(t)
	var n int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM record_chain_head`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("the verifier sees %d chain heads (err %v), want 2: it must read every tenant", n, err)
	}
	if _, err := pool.Exec(ctx(t), `UPDATE record_chain_head SET seq = 1 WHERE tenant_id = 'ten_a'`); err == nil {
		t.Fatal("the verifier updated a chain head: it must read only")
	}
}

// verifierPool opens a pool whose connections log in as a role that is a member of record_verify only: the chain verifier of REC-05. It has no
// tenant mapping, which is what makes it the one role that reads every tenant.
func (w *rowWorld) verifierPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	u, err := url.Parse(w.dsn)
	if err != nil {
		t.Fatal(err)
	}
	role := "record_verify__rs_" + strings.TrimPrefix(u.Path, "/")
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := w.admin.Exec(ctx(t), `CREATE ROLE `+quoted+` LOGIN IN ROLE record_verify`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if a, err := pgxpool.New(c, strings.Replace(w.dsn, "/"+strings.TrimPrefix(u.Path, "/")+"?", "/postgres?", 1)); err == nil {
			_, _ = a.Exec(c, `DROP ROLE IF EXISTS `+quoted)
			a.Close()
		}
	})
	u.User = url.User(role)
	pool, err := pgxpool.New(ctx(t), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Founder decision D5 (7 Nov 2026): the chain verifier may SET a tenant's kill switch on a proven break; only a person clears it. Today migration
// 0006 grants UPDATE (kill_switch) without a direction, so the verifier can also reopen a stopped tenant. FAILS on main until a rule
// (a trigger or a policy) allows false -> true for record_verify and nothing else for that role.
func TestKillSwitchTheVerifierMayStopATenantButNeverReopenIt(t *testing.T) {
	w := newRowWorld(t)
	pool := w.verifierPool(t)
	tag, err := pool.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = 'ten_a'`)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("control: the verifier could not stop ten_a: rows=%d err=%v; D5 says it may", tag.RowsAffected(), err)
	}
	tag, err = pool.Exec(ctx(t), `UPDATE tenant SET kill_switch = false WHERE tenant_id = 'ten_a'`)
	var stopped bool
	if qerr := w.admin.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = 'ten_a'`).Scan(&stopped); qerr != nil {
		t.Fatal(qerr)
	}
	if !stopped {
		t.Fatalf("the verifier reopened a stopped tenant (rows=%d err=%v); only a person clears the kill switch (D5)", tag.RowsAffected(), err)
	}
}
