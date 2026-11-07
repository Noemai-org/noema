package db_test

// TRU-05 slice 2 (migration 0009; Architect, 4 Nov 2026): the Decision Record is written before any grant, and a grant exists only for an
// ALLOW decision of the same tenant. REQ-TRU-008 acceptance: grants where the decision outcome is not ALLOW: 0, by a database trigger.
// REQ-TRU-001 / REQ-DATA-001: decision_record is insert-only and only Trust writes it. All rows are SYNTHETIC.

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

const grantFor = `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
	VALUES ('grt_x', 'ten_a', '%s', 'n_x', jsonb_build_object('decision_id', '%[1]s', 'tenant_id', 'ten_a'), 'pol_1', '1', now() + interval '1 hour', 'k1', 'aa')`

func decisionRow(id, tenant, outcome string) string {
	return fmt.Sprintf(`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		VALUES ('%s', '%s', jsonb_build_object('decision_id', '%s', 'tenant_id', '%s', 'outcome', '%s'), '%s', 'sha256:%064d', '[]', 'k1', 'aa')`, id, tenant, id, tenant, outcome, outcome, 1)
}

// Codex P1 on noema#168: the grant guard reads the outcome column, so a record whose signed document says something else would bypass it.
// The document and the columns the guard and the foreign keys read must agree.
func TestTheDecisionRecordDocumentMustAgreeWithItsColumns(t *testing.T) {
	pool, dsn := migratedDSN(t)
	if _, err := pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
	trust := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_a")
	insert := func(docID, docTenant, docOutcome string) error {
		return tryAs(t, trust, fmt.Sprintf(`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
			VALUES ('dec_m', 'ten_a', jsonb_build_object('decision_id', '%s', 'tenant_id', '%s', 'outcome', '%s'), 'ALLOW', 'sha256:%064d', '[]', 'k1', 'aa')`, docID, docTenant, docOutcome, 1))
	}
	wantCode(t, insert("dec_m", "ten_a", "REJECT"), violation, "the document says REJECT, the column ALLOW")
	wantCode(t, insert("dec_other", "ten_a", "ALLOW"), violation, "the document names another decision")
	wantCode(t, insert("dec_m", "ten_b", "ALLOW"), violation, "the document names another tenant")
	wantCode(t, tryAs(t, trust, `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		VALUES ('dec_m', 'ten_a', '{}', 'ALLOW', 'sha256:`+fmt.Sprintf("%064d", 1)+`', '[]', 'k1', 'aa')`), violation, "a document with no outcome, decision or tenant")
	if err := insert("dec_m", "ten_a", "ALLOW"); err != nil {
		t.Fatalf("a document that agrees with its columns: %v", err)
	}
}

func TestAGrantExistsOnlyForAnAllowDecisionOfTheSameTenant(t *testing.T) {
	pool, dsn := migratedDSN(t)
	for _, q := range []string{`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`, `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	trust := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_a")
	for _, o := range []string{"CLARIFY", "CONFIRM", "WAIT", "ESCALATE", "REJECT", "UNKNOWN"} {
		if _, err := pool.Exec(ctx(t), decisionRow("dec_"+o, "ten_a", o)); err != nil {
			t.Fatalf("%s: %v", o, err)
		}
		wantCode(t, tryAs(t, trust, fmt.Sprintf(grantFor, "dec_"+o)), violation, "a grant for a "+o+" decision")
	}
	if _, err := pool.Exec(ctx(t), decisionRow("dec_ALLOW", "ten_a", "ALLOW")); err != nil {
		t.Fatal(err)
	}
	if err := tryAs(t, trust, fmt.Sprintf(grantFor, "dec_ALLOW")); err != nil {
		t.Fatalf("a grant for an ALLOW decision: %v", err)
	}
	wantCode(t, tryAs(t, trust, fmt.Sprintf(grantFor, "dec_missing")), violation, "a grant with no decision record: the Record comes first")
	if _, err := pool.Exec(ctx(t), decisionRow("dec_OTHER", "ten_b", "ALLOW")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, tryAs(t, trust, fmt.Sprintf(grantFor, "dec_OTHER")), violation, "a grant for another tenant's ALLOW decision")
}

func TestTheDecisionRecordIsInsertOnlyAndOnlyTrustWritesIt(t *testing.T) {
	pool, dsn := migratedDSN(t)
	if _, err := pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
	trust := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_a")
	if _, err := pool.Exec(ctx(t), decisionRow("dec_1", "ten_a", "ALLOW")); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{`UPDATE decision_record SET outcome = 'REJECT'`, `DELETE FROM decision_record`, `TRUNCATE decision_record`} {
		wantCode(t, try(t, pool, "trust_rw", stmt), denied, "trust_rw holds no UPDATE, DELETE or TRUNCATE: "+stmt)
	}
	// and the owner, who holds every privilege, is stopped by the trigger
	for _, stmt := range []string{`UPDATE decision_record SET outcome = 'REJECT'`, `DELETE FROM decision_record`, `TRUNCATE decision_record`} {
		wantCode(t, try(t, pool, "postgres", stmt), violation, "the owner: "+stmt)
	}
	for _, role := range []string{"execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw"} {
		wantCode(t, try(t, pool, role, decisionRow("dec_2", "ten_a", "ALLOW")), denied, role+" inserting a Decision Record")
	}
	if err := tryAs(t, trust, decisionRow("dec_3", "ten_a", "ALLOW")); err != nil {
		t.Fatalf("trust_rw inserting a Decision Record: %v", err)
	}
	wantCode(t, tryAs(t, trust, decisionRow("dec_1", "ten_a", "ALLOW")), violation, "a second record with one decision_id")
	wantCode(t, tryAs(t, trust, decisionRow("dec_4", "ten_a", "MAYBE")), violation, "an outcome outside the seven")
	wantCode(t, tryAs(t, trust, decisionRow("dec_5", "ten_none", "ALLOW")), denied, "a record for a tenant that does not exist (the login is not mapped to it)")
	// every power that reads decisions can; the others cannot
	for role, want := range map[string]bool{"trust_rw": true, "execution_rw": true, "outcome_rw": true, "evidence_rw": false, "orchestrator_rw": false} {
		var got bool
		if err := pool.QueryRow(ctx(t), `SELECT has_table_privilege($1, 'decision_record', 'SELECT')`, role).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s SELECT on decision_record = %v, want %v", role, got, want)
		}
	}
}

// Codex P2 on noema#168: the guard reads the decision_id column, but Execution decodes the signed grant JSON. They must name the same
// decision and tenant, or a grant could be stored under an ALLOW decision while its signed document names a REJECT one.
func TestTheSignedGrantMustNameTheDecisionAndTenantOfItsColumns(t *testing.T) {
	pool, dsn := migratedDSN(t)
	for _, q := range []string{`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`, `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []struct{ id, outcome string }{{"dec_allow", "ALLOW"}, {"dec_reject", "REJECT"}} {
		if _, err := pool.Exec(ctx(t), decisionRow(d.id, "ten_a", d.outcome)); err != nil {
			t.Fatal(err)
		}
	}
	trust := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_a")
	insert := func(grantID, docJSON string) error {
		return tryAs(t, trust, fmt.Sprintf(`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
			VALUES ('%s', 'ten_a', 'dec_allow', 'n_%s', '%s'::jsonb, 'pol_1', '1', now() + interval '1 hour', 'k1', 'aa')`, grantID, grantID, docJSON))
	}
	wantCode(t, insert("grt_1", `{"decision_id":"dec_reject","tenant_id":"ten_a"}`), violation, "the column says the ALLOW decision, the signed document a REJECT one")
	wantCode(t, insert("grt_2", `{"decision_id":"dec_allow","tenant_id":"ten_b"}`), violation, "the signed document names another tenant")
	wantCode(t, insert("grt_3", `{"tenant_id":"ten_a"}`), violation, "the signed document names no decision")
	wantCode(t, insert("grt_4", `{}`), violation, "the signed document names neither decision nor tenant")
	if err := insert("grt_5", `{"decision_id":"dec_allow","tenant_id":"ten_a"}`); err != nil {
		t.Fatalf("a grant whose document agrees with its columns: %v", err)
	}
}

// Codex and review of noema#168 (REQ-SEC-004, INV-11): a Trust login writes a Decision Record only for its own tenant. A foreign key proves only that
// a tenant exists, so session_user is looked up in tenant_login, as for the Record (DEBT-04 option A) and for evidence (noema#167).
func TestATrustLoginWritesDecisionRecordsOnlyForItsOwnTenant(t *testing.T) {
	pool, dsn := migratedDSN(t)
	for _, q := range []string{`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`, `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	a := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_a")
	if _, err := a.Exec(ctx(t), decisionRow("dec_own", "ten_a", "ALLOW")); err != nil {
		t.Fatalf("tenant A's Trust login, its own tenant: %v", err)
	}
	_, err := a.Exec(ctx(t), decisionRow("dec_foreign", "ten_b", "ALLOW"))
	wantCode(t, err, denied, "tenant A's Trust login writing an ALLOW decision for tenant B")
	// a login with the Trust role but no tenant mapping writes nothing
	u := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_b")
	if _, err := pool.Exec(ctx(t), `DELETE FROM tenant_login WHERE tenant_id = 'ten_b' AND power = 'trust'`); err != nil {
		t.Fatal(err)
	}
	_, err = u.Exec(ctx(t), decisionRow("dec_unmapped", "ten_a", "ALLOW")) // tenant A has a mapped Trust login; that must not open the door for this one
	wantCode(t, err, denied, "a Trust login with no tenant mapping")
	// a login mapped to tenant A as another power that was also handed the Trust role is not a Trust login
	o := testpg.TenantLogin(t, pool, dsn, "outcome_rw", "ten_a")
	if _, err := pool.Exec(ctx(t), `GRANT trust_rw TO `+fmt.Sprintf("%q", currentUser(t, o))); err != nil {
		t.Fatal(err)
	}
	_, err = o.Exec(ctx(t), decisionRow("dec_other_power", "ten_a", "ALLOW"))
	wantCode(t, err, denied, "a login mapped as another power")
	// and a record that was refused left nothing behind, so no grant can rest on it
	var n int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM decision_record WHERE decision_id IN ('dec_foreign', 'dec_unmapped', 'dec_other_power')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("refused records stored: %d (%v)", n, err)
	}
}

func currentUser(t *testing.T, pool interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) string {
	t.Helper()
	var u string
	if err := pool.QueryRow(ctx(t), `SELECT session_user::text`).Scan(&u); err != nil {
		t.Fatal(err)
	}
	return u
}

// Review of noema#168 at cd471c6: a table-wide SELECT would let any tenant's Trust, Execution or Outcome login read every tenant's decisions (policy versions,
// reasons, evidence snapshot). Reads are bound to the login's tenant by row-level security on the same session_user mapping (as noema#167 does for evidence).
func TestAPowerLoginReadsOnlyItsOwnTenantsDecisions(t *testing.T) {
	pool, dsn := migratedDSN(t)
	for _, q := range []string{`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`, `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	trustA := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_a")
	if _, err := trustA.Exec(ctx(t), decisionRow("dec_a", "ten_a", "ALLOW")); err != nil {
		t.Fatal(err)
	}
	if _, err := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_b").Exec(ctx(t), decisionRow("dec_b", "ten_b", "ALLOW")); err != nil {
		t.Fatal(err)
	}
	count := func(p interface {
		QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	}, where string) int {
		var n int
		if err := p.QueryRow(ctx(t), `SELECT count(*) FROM decision_record `+where).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, group := range []string{"trust_rw", "execution_rw", "outcome_rw"} {
		var p interface {
			QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
		}
		if group == "trust_rw" {
			p = trustA
		} else {
			p = testpg.TenantLogin(t, pool, dsn, group, "ten_a")
		}
		if n := count(p, ""); n != 1 {
			t.Errorf("%s of tenant A sees %d decision records, want exactly its own 1", group, n)
		}
		if n := count(p, "WHERE tenant_id = 'ten_b'"); n != 0 {
			t.Errorf("%s of tenant A reads %d of tenant B's decisions", group, n)
		}
	}
	// the administrator, who owns the table, still sees both (the audit and migration path)
	if n := count(pool, ""); n != 2 {
		t.Errorf("the administrator sees %d, want 2", n)
	}
	// and a login that is not mapped to any tenant sees none
	un := testpg.TenantLogin(t, pool, dsn, "outcome_rw", "ten_b")
	if _, err := pool.Exec(ctx(t), `DELETE FROM tenant_login WHERE tenant_id = 'ten_b' AND power = 'outcome'`); err != nil {
		t.Fatal(err)
	}
	if n := count(un, ""); n != 0 {
		t.Errorf("an unmapped Outcome login sees %d decision records, want 0", n)
	}
}

// tryAs runs one statement as the pool's login in a transaction that is rolled back.
func tryAs(t *testing.T, p *pgxpool.Pool, stmt string) error {
	t.Helper()
	tx, err := p.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	_, err = tx.Exec(ctx(t), stmt)
	return err
}

// Codex P2 on noema#168 at eaf5df2: the link from an action decision to its ESCALATE disposition (REQ-TRU-010) must stay inside the tenant. A foreign key
// on the decision ID alone accepts another tenant's decision.
func TestADispositionLinkNeverCrossesTenants(t *testing.T) {
	pool, dsn := migratedDSN(t)
	for _, q := range []string{`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`, `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx(t), decisionRow("dec_disp_a", "ten_a", "ESCALATE")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx(t), decisionRow("dec_disp_b", "ten_b", "ESCALATE")); err != nil {
		t.Fatal(err)
	}
	trust := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_a")
	linked := func(id, disposition string) string {
		return fmt.Sprintf(`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, disposition_decision_id, intent_hash, policy_versions_json, sig_key_id, sig_value)
			VALUES ('%s', 'ten_a', jsonb_build_object('decision_id', '%s', 'tenant_id', 'ten_a', 'outcome', 'ALLOW'), 'ALLOW', '%s', 'sha256:%064d', '[]', 'k1', 'aa')`, id, id, disposition, 1)
	}
	wantCode(t, tryAs(t, trust, linked("dec_x", "dec_disp_b")), violation, "tenant A's decision linked to tenant B's disposition")
	wantCode(t, tryAs(t, trust, linked("dec_y", "dec_missing")), violation, "a link to a decision that does not exist")
	// Codex P2 at 0b22608: decision-record.v1 names the ESCALATE disposition decision, so a same-tenant target with another outcome is refused too
	for _, o := range []string{"ALLOW", "REJECT", "CONFIRM"} {
		if _, err := pool.Exec(ctx(t), decisionRow("dec_plain_"+o, "ten_a", o)); err != nil {
			t.Fatal(err)
		}
		wantCode(t, tryAs(t, trust, linked("dec_w_"+o, "dec_plain_"+o)), violation, "a link to a same-tenant "+o+" decision, which is not a disposition")
	}
	if err := tryAs(t, trust, linked("dec_z", "dec_disp_a")); err != nil {
		t.Fatalf("a link to the tenant's own disposition: %v", err)
	}
}

// A6 (rulings of 6 Nov 2026, per noema#167): decision_record and decision_grant are bound to the login's tenant by row-level security, and the
// policies are FORCED so that the table's owner is subject to them too (a superuser still bypasses, and no runtime login is one).
func TestDecisionRecordAndDecisionGrantAreTenantScopedAndForced(t *testing.T) {
	pool, dsn := migratedDSN(t)
	for _, tb := range []string{"decision_record", "decision_grant"} {
		var on, forced bool
		if err := pool.QueryRow(ctx(t), `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`, tb).Scan(&on, &forced); err != nil {
			t.Fatal(err)
		}
		if !on || !forced {
			t.Errorf("%s: row-level security enabled %v, forced %v; want both", tb, on, forced)
		}
	}
	for _, q := range []string{`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`, `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	trustA := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_a")
	trustB := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_b")
	for _, d := range []struct {
		p            *pgxpool.Pool
		id, tenant   string
		grant, nonce string
	}{{trustA, "dec_a", "ten_a", "grt_a", "n_a"}, {trustB, "dec_b", "ten_b", "grt_b", "n_b"}} {
		if _, err := d.p.Exec(ctx(t), decisionRow(d.id, d.tenant, "ALLOW")); err != nil {
			t.Fatal(err)
		}
		if _, err := d.p.Exec(ctx(t), fmt.Sprintf(`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
			VALUES ('%s', '%s', '%s', '%s', jsonb_build_object('decision_id', '%s', 'tenant_id', '%s'), 'pol_1', '1', now() + interval '1 hour', 'k1', 'aa')`,
			d.grant, d.tenant, d.id, d.nonce, d.id, d.tenant)); err != nil {
			t.Fatalf("tenant %s writing its own grant: %v", d.tenant, err)
		}
	}
	// a Trust login cannot write a grant for another tenant, even one that names a decision of that tenant
	_, err := trustA.Exec(ctx(t), `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		VALUES ('grt_x', 'ten_b', 'dec_b', 'n_x', jsonb_build_object('decision_id', 'dec_b', 'tenant_id', 'ten_b'), 'pol_1', '1', now() + interval '1 hour', 'k1', 'aa')`)
	// the grant trigger, which fires before the row-level check, already finds no ALLOW decision of tenant B visible to tenant A; either refusal is the point
	wantCode(t, err, violation, "tenant A's Trust login writing a grant for tenant B")
	execA := testpg.TenantLogin(t, pool, dsn, "execution_rw", "ten_a")
	logins := map[string]*pgxpool.Pool{"trust_rw": trustA, "execution_rw": execA, "outcome_rw": testpg.TenantLogin(t, pool, dsn, "outcome_rw", "ten_a")}
	for group, p := range logins {
		var own, foreign int
		if err := p.QueryRow(ctx(t), `SELECT count(*) FILTER (WHERE tenant_id = 'ten_a'), count(*) FILTER (WHERE tenant_id = 'ten_b') FROM decision_grant`).Scan(&own, &foreign); err != nil {
			t.Fatal(err)
		}
		if own != 1 || foreign != 0 {
			t.Errorf("%s of tenant A sees %d own and %d foreign grants, want 1 and 0", group, own, foreign)
		}
	}
	// Execution moves the status of its own tenant's grant only
	if tag, err := execA.Exec(ctx(t), `UPDATE decision_grant SET status = 'VOID' WHERE grant_id = 'grt_b'`); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("tenant A's Execution login changed %d of tenant B's grants (%v)", tag.RowsAffected(), err)
	}
	if tag, err := execA.Exec(ctx(t), `UPDATE decision_grant SET status = 'VOID' WHERE grant_id = 'grt_a'`); err != nil || tag.RowsAffected() != 1 {
		t.Errorf("tenant A's Execution login changed %d of its own grants (%v)", tag.RowsAffected(), err)
	}
}
