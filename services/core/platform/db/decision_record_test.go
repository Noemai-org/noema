package db_test

// TRU-05 slice 2 (migration 0008; Architect, 4 Nov 2026): the Decision Record is written before any grant, and a grant exists only for an
// ALLOW decision of the same tenant. REQ-TRU-008 acceptance: grants where the decision outcome is not ALLOW: 0, by a database trigger.
// REQ-TRU-001 / REQ-DATA-001: decision_record is insert-only and only Trust writes it. All rows are SYNTHETIC.

import (
	"fmt"
	"testing"
)

const grantFor = `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
	VALUES ('grt_x', 'ten_a', '%s', 'n_x', '{}', 'pol_1', '1', now() + interval '1 hour', 'k1', 'aa')`

func decisionRow(id, tenant, outcome string) string {
	return fmt.Sprintf(`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		VALUES ('%s', '%s', '{}', '%s', 'sha256:%064d', '[]', 'k1', 'aa')`, id, tenant, outcome, 1)
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
		wantCode(t, try(t, pool, "trust_rw", stmt), violation, "trust_rw: "+stmt)
	}
	for _, stmt := range []string{`UPDATE decision_record SET outcome = 'REJECT'`, `DELETE FROM decision_record`, `TRUNCATE decision_record`} {
		if err := try(t, pool, "postgres", stmt); err == nil {
			t.Errorf("the owner changed the Decision Record: %s", stmt)
		}
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
