package db_test

// A165 / noema#462 (EL condition 3 on the evidence_verify exception, noema#282): no function in the public schema is executable by PUBLIC. A function created without a REVOKE
// is executable by every role, so a login that should hold "EXECUTE on no function" would hold it. Trigger functions are skipped: they cannot be called directly. Migration
// 0015's policy_version_key(text) was the one function that had no REVOKE; trust_rw keeps EXECUTE on it because the CHECK constraint policy_publication_key_is_the_version is
// evaluated by the inserting role. REQ-SEC-004, AGENT-15 (forward-only migrations). All data is SYNTHETIC.

import "testing"

func TestNoNonTriggerFunctionInPublicIsExecutableByPublic(t *testing.T) {
	pool := migrated(t)
	rows, err := pool.Query(ctx(t), `SELECT p.oid::regprocedure::text FROM pg_proc p
		WHERE p.pronamespace = 'public'::regnamespace AND p.prokind = 'f' AND p.prorettype <> 'trigger'::regtype
		  AND has_function_privilege('public', p.oid, 'EXECUTE') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var sig string
		if err := rows.Scan(&sig); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s is executable by PUBLIC", sig)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// The revoke must not take away what the CHECK needs: the Trust role that inserts a publication still evaluates policy_version_key; no other application role can call it.
func TestOnlyTheTrustRoleCanExecutePolicyVersionKey(t *testing.T) {
	pool := migrated(t)
	for role, want := range map[string]bool{"trust_rw": true, "execution_rw": false, "outcome_rw": false, "record_read": false} {
		var can bool
		if err := pool.QueryRow(ctx(t), `SELECT has_function_privilege($1, 'public.policy_version_key(text)', 'EXECUTE')`, role).Scan(&can); err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if can != want {
			t.Errorf("%s can execute policy_version_key = %v, want %v", role, can, want)
		}
	}
}
