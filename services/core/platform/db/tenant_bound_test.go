package db_test

// DEBT-04 option A, slice 1 (REQ-SEC-004, INV-12, REQ-AUD-001, REQ-AUD-003): the tenant of a Record call comes from the
// login the connection authenticated as, never from an argument. These tests connect as real LOGIN roles, each mapped to
// one tenant, because SET ROLE would leave session_user as the administrator and prove nothing. All data is SYNTHETIC.

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

type tenantWorld struct {
	admin   *pgxpool.Pool
	dsn     string
	suffix  string                  // login roles are cluster-wide, so each test database gets its own names
	signers map[string]chain.Signer // a SYNTHETIC key for every signing identity (REC-08)
}

// name is the login role for a power and a tenant in this world.
func (w *tenantWorld) name(power, tenant string) string {
	return power + "__" + tenant + "_" + w.suffix
}

func newTenantWorld(t *testing.T) *tenantWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	w := &tenantWorld{admin: admin, dsn: dsn, suffix: strings.TrimPrefix(u.Path, "/"), signers: recordsig.All(t, admin, "db", nil)}
	roles := map[string]string{ // login role -> group role
		w.name("execution_rw", "ten_a"): "execution_rw", w.name("execution_rw", "ten_b"): "execution_rw", w.name("execution_rw", "unmapped"): "execution_rw",
		w.name("record_read", "ten_a"): "record_read", w.name("record_read", "ten_b"): "record_read",
	}
	for role, group := range roles {
		if _, err := admin.Exec(ctx(t), `CREATE ROLE `+pgx.Identifier{role}.Sanitize()+` LOGIN IN ROLE `+group); err != nil {
			t.Fatalf("create %s: %v", role, err)
		}
		role := role
		t.Cleanup(func() {
			c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if a, err := pgxpool.New(c, strings.Replace(dsn, "/"+w.suffix+"?", "/postgres?", 1)); err == nil {
				_, _ = a.Exec(c, `DROP ROLE IF EXISTS `+pgx.Identifier{role}.Sanitize())
				a.Close()
			}
		})
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A'), ('ten_b', 'B')`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant_login (login_role, power, tenant_id) VALUES ($1, 'execution', 'ten_a'), ($2, 'execution', 'ten_b'), ($3, 'record_read', 'ten_a'), ($4, 'record_read', 'ten_b')`,
		w.name("execution_rw", "ten_a"), w.name("execution_rw", "ten_b"), w.name("record_read", "ten_a"), w.name("record_read", "ten_b")); err != nil {
		t.Fatal(err)
	}
	return w
}

// login opens a pool whose connections authenticate as the given LOGIN role.
func (w *tenantWorld) login(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	u, err := url.Parse(w.dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User(role)
	pool, err := pgxpool.New(ctx(t), u.String())
	if err != nil {
		t.Fatalf("login as %s: %v", role, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// signedInsert calls record_insert_t inside tx with a body that names bodyTenant and, when the emitter holds a key in this world, that emitter's SYNTHETIC
// signature over the exact bytes of the body (REC-08). An emitter with no key sends no signature: the checks of the function that come before the trigger
// (the emitter bound to the login, the tenant of the login, the head) refuse it first, which is what those tests assert.
func (w *tenantWorld) signedInsert(t *testing.T, tx pgx.Tx, id string, seq int64, prev, hash, emitter, bodyTenant string, at time.Time) error {
	t.Helper()
	body := map[string]any{"schema_version": "3.0", "tenant_id": bodyTenant, "correlation_id": "corr_1", "event_id": id, "seq": seq, "prev_hash": prev, "written_by": emitter,
		"actor": emitter, "event_type": "GRANT_SPENT", "object_ref": "grt_1", "payload_hash": "sha256:p", "at": at.UTC().Format("2006-01-02T15:04:05.000Z")}
	canon, err := crypto.CanonicalJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	var keyID, sig *string
	var signed []byte
	if sgn, ok := w.signers[emitter]; ok {
		k, v, jcs, err := sgn.SignRecordEvent(body)
		if err != nil {
			t.Fatal(err)
		}
		keyID, sig, signed = &k, &v, jcs
	}
	_, err = tx.Exec(ctx(t), `SELECT record_insert_t($1, $2, $3, $4, $5, 'GRANT_SPENT', 'grt_1', 'sha256:p', 'corr_1', $6, '3.0', $5, $7::jsonb, NULL, $8, $9, $10)`,
		id, seq, prev, hash, emitter, at, string(canon), keyID, sig, signed)
	return err
}

// appendAs appends one event through the login's own tenant-bound functions and returns the new head hash.
func (w *tenantWorld) appendAs(t *testing.T, pool *pgxpool.Pool, tenant, id string, seqWant int64, hash string) {
	t.Helper()
	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	var seq int64
	var head string
	if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &head); err != nil {
		t.Fatalf("lock head: %v", err)
	}
	if seq+1 != seqWant {
		t.Fatalf("head seq %d, want %d", seq, seqWant-1)
	}
	if err := w.signedInsert(t, tx, id, seqWant, head, hash, "core-execution", tenant, time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
}

func hashOf(n string) string { return "sha256:" + strings.Repeat(n, 64) }

// A login writes to its own tenant's chain, whatever else is in the database, and two tenants' chains stay separate.
func TestTenantBoundWritesGoToTheLoginsOwnTenant(t *testing.T) {
	w := newTenantWorld(t)
	a, b := w.login(t, w.name("execution_rw", "ten_a")), w.login(t, w.name("execution_rw", "ten_b"))
	w.appendAs(t, a, "ten_a", "recev_a1", 1, hashOf("1"))
	w.appendAs(t, a, "ten_a", "recev_a2", 2, hashOf("2"))
	w.appendAs(t, b, "ten_b", "recev_b1", 1, hashOf("3"))
	rows, err := w.admin.Query(ctx(t), `SELECT event_id, tenant_id FROM record_event ORDER BY event_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var id, ten string
		if err := rows.Scan(&id, &ten); err != nil {
			t.Fatal(err)
		}
		got[id] = ten
	}
	want := map[string]string{"recev_a1": "ten_a", "recev_a2": "ten_a", "recev_b1": "ten_b"}
	if len(got) != len(want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	for id, ten := range want {
		if got[id] != ten {
			t.Fatalf("event %s is in tenant %q, want %q", id, got[id], ten)
		}
	}
}

// The tenant-bound functions have no tenant argument, so there is nothing for a caller to choose.
func TestTenantBoundFunctionsTakeNoTenantArgument(t *testing.T) {
	w := newTenantWorld(t)
	rows, err := w.admin.Query(ctx(t), `SELECT p.proname, coalesce(array_to_string(p.proargnames, ','), '') FROM pg_proc p
		WHERE p.proname IN ('record_lock_head_t', 'record_insert_t', 'record_read_chain_t', 'record_login_tenant') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var name, args string
		if err := rows.Scan(&name, &args); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(args, "tenant") && name != "record_read_chain_t" {
			t.Errorf("%s has an argument that names a tenant: %s", name, args)
		}
		n++
	}
	if n != 4 {
		t.Fatalf("found %d of the 4 functions", n)
	}
}

// A read login sees its own tenant's chain and only that. This is the cross-tenant denial of DEBT-04 for the new path.
func TestTenantBoundReadSeesOnlyTheLoginsTenant(t *testing.T) {
	w := newTenantWorld(t)
	a, b := w.login(t, w.name("execution_rw", "ten_a")), w.login(t, w.name("execution_rw", "ten_b"))
	w.appendAs(t, a, "ten_a", "recev_a1", 1, hashOf("1"))
	w.appendAs(t, a, "ten_a", "recev_a2", 2, hashOf("2"))
	w.appendAs(t, b, "ten_b", "recev_b1", 1, hashOf("3"))
	for role, want := range map[string][]string{w.name("record_read", "ten_a"): {"recev_a1", "recev_a2"}, w.name("record_read", "ten_b"): {"recev_b1"}} {
		rows, err := w.login(t, role).Query(ctx(t), `SELECT event_id FROM record_read_chain_t()`)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		var got []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			got = append(got, id)
		}
		rows.Close()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s read %v, want %v", role, got, want)
		}
	}
}

// Refusals under the real logins: no mapping, the wrong kind of call, direct table access, the mapping table, and the ways a
// caller might try to become another login or tenant.
func TestTenantBoundCallsAreRefusedForTheWrongLogin(t *testing.T) {
	w := newTenantWorld(t)
	xa, xb, un := w.name("execution_rw", "ten_a"), w.name("execution_rw", "ten_b"), w.name("execution_rw", "unmapped")
	ra := w.name("record_read", "ten_a")
	q := func(r string) string { return pgx.Identifier{r}.Sanitize() }
	cases := []struct{ name, role, stmt string }{
		{"an unmapped login cannot lock a head", un, `SELECT * FROM record_lock_head_t()`},
		{"a read login cannot write", ra, `SELECT * FROM record_lock_head_t()`},
		{"a write login cannot read the chain", xa, `SELECT * FROM record_read_chain_t()`},
		{"the mapping table cannot be read", xa, `SELECT * FROM tenant_login`},
		{"the mapping cannot be changed", xa, `UPDATE tenant_login SET tenant_id = 'ten_b' WHERE login_role = '` + xa + `'`},
		{"a mapping cannot be added", xa, `INSERT INTO tenant_login VALUES ('` + un + `', 'execution', 'ten_a')`},
		{"the resolver cannot be called directly", xa, `SELECT record_login_tenant('write')`},
		{"the Record tables cannot be read", xa, `SELECT * FROM record_event`},
		{"session authorization cannot be switched", xa, `SET SESSION AUTHORIZATION ` + q(xb)},
		{"another tenant's login cannot be assumed", xa, `SET ROLE ` + q(xb)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := w.login(t, c.role).Exec(ctx(t), c.stmt)
			wantCode(t, err, denied, c.name)
		})
	}
}

// Switching to the group role changes current_user, not session_user: the tenant stays the login's.
func TestSetRoleToTheGroupRoleDoesNotChangeTheTenant(t *testing.T) {
	w := newTenantWorld(t)
	a := w.login(t, w.name("execution_rw", "ten_a"))
	conn, err := a.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx(t), `SET ROLE execution_rw`); err != nil {
		t.Fatal(err)
	}
	var seq int64
	var head string
	if err := conn.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &head); err != nil {
		t.Fatal(err)
	}
	var tenant string
	if err := w.admin.QueryRow(ctx(t), `SELECT tenant_id FROM record_chain_head WHERE hash = $1`, head).Scan(&tenant); err != nil || tenant != "ten_a" {
		t.Fatalf("after SET ROLE the head belongs to %q (%v), want ten_a", tenant, err)
	}
}

// An event whose hashed body names another tenant than the login's is refused by the database, so the chain of the login's
// tenant can never hold an event that says it belongs to another (it would otherwise break verification).
func TestAnEventNamingAnotherTenantIsRefused(t *testing.T) {
	w := newTenantWorld(t)
	a := w.login(t, w.name("execution_rw", "ten_a"))
	tx, err := a.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	var seq int64
	var head string
	if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &head); err != nil {
		t.Fatal(err)
	}
	err = w.signedInsert(t, tx, "recev_x1", seq+1, head, hashOf("9"), "core-execution", "ten_b", time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	wantCode(t, err, denied, "an event naming ten_b written by ten_a's login")
}

// Security review of noema#145, finding 1 (REQ-SEC-004, INV-12, REQ-AUD-003): record_insert_t refuses a written_by that is not
// the emitter of the login's power. Without this, a login of one power could write Record events in another power's name.
// Powers map to emitters as PERD 41.1 names them: trust to core-trust, execution to core-execution, outcome to core-outcome,
// evidence to core-evidence, orchestrator to core-workflows. Every other emitter (DEBT-07, docs#106 R16) is refused to a power login and can be written only by a login mapped to that emitter.
func TestRecordInsertRefusesAnEmitterThatIsNotTheLoginsPower(t *testing.T) {
	w := newTenantWorld(t)
	a := w.login(t, w.name("execution_rw", "ten_a"))
	for _, emitter := range []string{"core-trust", "core-outcome", "core-evidence", "core-workflows", "core-edge", "core-record", "admin", "model-service", "mock-ledger", "unknown-emitter", "TRUST", ""} {
		t.Run("execution login as "+emitter, func(t *testing.T) {
			tx, err := a.Begin(ctx(t))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx(t)) }()
			var seq int64
			var head string
			if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &head); err != nil {
				t.Fatal(err)
			}
			err = w.signedInsert(t, tx, "recev_x", seq+1, head, hashOf("9"), emitter, "ten_a", time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
			wantCode(t, err, denied, "execution login writing as "+emitter)
		})
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM record_event`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a refused insert left %d rows (%v)", n, err)
	}
}

// DEBT-07 (docs#106 R16, Codex review of noema#145): an emitter that is not a power is written only by a login that the
// mapping binds to that emitter, and such a login writes nothing else. Cross-emitter forgery is refused both ways.
func TestAnEmitterThatIsNotAPowerIsWrittenOnlyByItsOwnBoundLogin(t *testing.T) {
	w := newTenantWorld(t)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_c', 'C')`); err != nil {
		t.Fatal(err)
	}
	edge := testpg.EmitterLogin(t, w.admin, w.dsn, "ten_c", "core-edge")
	one := func(pool *pgxpool.Pool, emitter string) error {
		tx, err := pool.Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx(t)) }()
		var seq int64
		var head string
		if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &head); err != nil {
			t.Fatal(err)
		}
		return w.signedInsert(t, tx, "recev_y", seq+1, head, hashOf("8"), emitter, "ten_c", time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	}
	if err := one(edge, "core-edge"); err != nil {
		t.Fatalf("the login bound to core-edge could not write as core-edge: %v", err)
	}
	for _, other := range []string{"core-execution", "core-trust", "admin", "model-service", "mock-ledger", "TRUST", ""} {
		wantCode(t, one(edge, other), denied, "the core-edge login writing as "+other)
	}
}

// Codex review of noema#145 (head 94e6299): a login bound to an emitter that is not a power holds the Record append functions
// and nothing else. It is not a member of any power role, so it cannot act as that power (four-power separation, INV-29).
func TestAnEmitterLoginHoldsNothingButTheRecordAppendFunctions(t *testing.T) {
	w := newTenantWorld(t)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_c', 'C')`); err != nil {
		t.Fatal(err)
	}
	edge := testpg.EmitterLogin(t, w.admin, w.dsn, "ten_c", "core-edge")
	for _, stmt := range []string{
		`SELECT * FROM grant_spend`,
		`SELECT * FROM execution_receipt`,
		`SELECT * FROM decision_grant`,
		`UPDATE decision_grant SET tenant_id = tenant_id`,
		`INSERT INTO grant_spend (tenant_id) VALUES ('ten_c')`,
		`SELECT * FROM record_event`,
		`SELECT * FROM record_chain_head`,
		`SELECT * FROM tenant_login`,
		`SELECT * FROM record_read_chain_t()`,
		`SELECT provision_tenant_login('x', 'execution', 'ten_c')`,
	} {
		_, err := edge.Exec(ctx(t), stmt)
		wantCode(t, err, denied, "the core-edge login: "+stmt)
	}
	var member bool
	if err := w.admin.QueryRow(ctx(t), `SELECT EXISTS (SELECT 1 FROM pg_auth_members m JOIN pg_roles r ON r.oid = m.roleid JOIN pg_roles u ON u.oid = m.member
		WHERE u.rolname LIKE 'record_emitter\_\_ten_c%' AND r.rolname IN ('trust_rw','execution_rw','outcome_rw','evidence_rw','orchestrator_rw','record_read','record_append','record_verify'))`).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if member {
		t.Fatal("an emitter login is a member of a power role")
	}
}

// suffixOf is the per-database suffix of the world's role names, read from the DSN when the world was not built by newTenantWorld.
func (w *tenantWorld) suffixOf(t *testing.T) string {
	t.Helper()
	if w.suffix != "" {
		return w.suffix
	}
	u, err := url.Parse(w.dsn)
	if err != nil {
		t.Fatal(err)
	}
	w.suffix = strings.TrimPrefix(u.Path, "/")
	return w.suffix
}
