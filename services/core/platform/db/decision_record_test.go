package db_test

// TRU-05 slice 2 (migration 0009; Architect, 4 Nov 2026): the Decision Record is written before any grant, and a grant exists only for an
// ALLOW decision of the same tenant. REQ-TRU-008 acceptance: grants where the decision outcome is not ALLOW: 0, by a database trigger.
// REQ-TRU-001 / REQ-DATA-001: decision_record is insert-only and only Trust writes it. All rows are SYNTHETIC.

import (
	"fmt"
	"testing"

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
	pool := migrated(t)
	if _, err := pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
	insert := func(docID, docTenant, docOutcome string) error {
		return try(t, pool, "trust_rw", fmt.Sprintf(`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
			VALUES ('dec_m', 'ten_a', jsonb_build_object('decision_id', '%s', 'tenant_id', '%s', 'outcome', '%s'), 'ALLOW', 'sha256:%064d', '[]', 'k1', 'aa')`, docID, docTenant, docOutcome, 1))
	}
	wantCode(t, insert("dec_m", "ten_a", "REJECT"), violation, "the document says REJECT, the column ALLOW")
	wantCode(t, insert("dec_other", "ten_a", "ALLOW"), violation, "the document names another decision")
	wantCode(t, insert("dec_m", "ten_b", "ALLOW"), violation, "the document names another tenant")
	wantCode(t, try(t, pool, "trust_rw", `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		VALUES ('dec_m', 'ten_a', '{}', 'ALLOW', 'sha256:`+fmt.Sprintf("%064d", 1)+`', '[]', 'k1', 'aa')`), violation, "a document with no outcome, decision or tenant")
	if err := insert("dec_m", "ten_a", "ALLOW"); err != nil {
		t.Fatalf("a document that agrees with its columns: %v", err)
	}
}

func TestAGrantExistsOnlyForAnAllowDecisionOfTheSameTenant(t *testing.T) {
	pool := migrated(t)
	for _, q := range []string{`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`, `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range []string{"CLARIFY", "CONFIRM", "WAIT", "ESCALATE", "REJECT", "UNKNOWN"} {
		if _, err := pool.Exec(ctx(t), decisionRow("dec_"+o, "ten_a", o)); err != nil {
			t.Fatalf("%s: %v", o, err)
		}
		wantCode(t, try(t, pool, "trust_rw", fmt.Sprintf(grantFor, "dec_"+o)), violation, "a grant for a "+o+" decision")
	}
	if _, err := pool.Exec(ctx(t), decisionRow("dec_ALLOW", "ten_a", "ALLOW")); err != nil {
		t.Fatal(err)
	}
	if err := try(t, pool, "trust_rw", fmt.Sprintf(grantFor, "dec_ALLOW")); err != nil {
		t.Fatalf("a grant for an ALLOW decision: %v", err)
	}
	wantCode(t, try(t, pool, "trust_rw", fmt.Sprintf(grantFor, "dec_missing")), violation, "a grant with no decision record: the Record comes first")
	if _, err := pool.Exec(ctx(t), decisionRow("dec_OTHER", "ten_b", "ALLOW")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, try(t, pool, "trust_rw", fmt.Sprintf(grantFor, "dec_OTHER")), violation, "a grant for another tenant's ALLOW decision")
}

func TestTheDecisionRecordIsInsertOnlyAndOnlyTrustWritesIt(t *testing.T) {
	pool := migrated(t)
	if _, err := pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
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
	if err := try(t, pool, "trust_rw", decisionRow("dec_3", "ten_a", "ALLOW")); err != nil {
		t.Fatalf("trust_rw inserting a Decision Record: %v", err)
	}
	wantCode(t, try(t, pool, "trust_rw", decisionRow("dec_1", "ten_a", "ALLOW")), violation, "a second record with one decision_id")
	wantCode(t, try(t, pool, "trust_rw", decisionRow("dec_4", "ten_a", "MAYBE")), violation, "an outcome outside the seven")
	wantCode(t, try(t, pool, "trust_rw", decisionRow("dec_5", "ten_none", "ALLOW")), violation, "a record for a tenant that does not exist")
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
	pool := migrated(t)
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
	insert := func(grantID, docJSON string) error {
		return try(t, pool, "trust_rw", fmt.Sprintf(`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
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
	_, err = u.Exec(ctx(t), decisionRow("dec_unmapped", "ten_b", "ALLOW"))
	wantCode(t, err, denied, "a Trust login with no tenant mapping")
	// and a record that was refused left nothing behind, so no grant can rest on it
	var n int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM decision_record WHERE decision_id IN ('dec_foreign', 'dec_unmapped')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("refused records stored: %d (%v)", n, err)
	}
}
