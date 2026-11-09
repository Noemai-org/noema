package db_test

// Ruling A20 (6 Nov 2026), REQ-AUD-001, REQ-AUD-003, REQ-SEC-004, INV-10, INV-11: audit reads the Record through read functions only, and neither audit
// nor any operator role has a path to a Record writer. The tests read the privileges the database holds (has_table_privilege, has_function_privilege),
// for every role a service can act as. They were NOT written failing-first: the privileges already hold on main (migrations 0001, 0003, 0006). They
// are the repeatable form of the list of paths checked for the Security Reviewer (docs/security/record-paths-audit.md); a mutation run (a grant added
// in a scratch database) turns each one red. All data is SYNTHETIC.

import (
	"fmt"
	"slices"
	"testing"
)

var recordTables = []string{"record_event", "record_chain_head", "tenant_login"}
var writeVerbs = []string{"INSERT", "UPDATE", "DELETE", "TRUNCATE"}
var recordWriters = []string{"record_insert_t", "record_lock_head_t", "provision_tenant_login", "record_login_tenant"}

func hasTablePriv(t *testing.T, role, table, verb string) bool {
	t.Helper()
	pool := migrated(t)
	var ok bool
	if err := pool.QueryRow(ctx(t), fmt.Sprintf(`SELECT has_table_privilege('%s', 'public.%s', '%s')`, role, table, verb)).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// executors returns every role in the list that can execute the named function.
func executors(t *testing.T, fn string) []string {
	t.Helper()
	pool := migrated(t)
	var exists int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM pg_proc WHERE pronamespace = 'public'::regnamespace AND proname = $1`, fn).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists == 0 {
		t.Fatalf("function %s does not exist: an empty executor list would be read as 'nobody can call it' and count as evidence", fn)
	}
	rows, err := pool.Query(ctx(t), `SELECT r.rolname FROM pg_roles r, pg_proc p
		WHERE p.pronamespace = 'public'::regnamespace AND p.proname = $1 AND has_function_privilege(r.rolname, p.oid, 'EXECUTE')
		  AND r.rolname = ANY($2) ORDER BY 1`, fn, runtimeRoles)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		out = append(out, n)
	}
	return out
}

// Audit (record_read) reads through record_read_chain_t and through nothing else: no privilege of any kind on the Record tables or the login map.
func TestAuditReadsTheRecordThroughTheReadFunctionOnly(t *testing.T) {
	for _, tbl := range recordTables {
		for _, verb := range append([]string{"SELECT"}, writeVerbs...) {
			if hasTablePriv(t, "record_read", tbl, verb) {
				t.Errorf("record_read holds %s on %s: audit would read or write the Record without the read function", verb, tbl)
			}
		}
	}
	if got := executors(t, "record_read_chain_t"); !slices.Contains(got, "record_read") {
		t.Errorf("record_read cannot execute record_read_chain_t: %v", got)
	}
}

// No audit, verifier or operator-facing role reaches a Record writer or the login map's writer.
func TestNoAuditOrVerifierRoleCanExecuteARecordWriter(t *testing.T) {
	for _, fn := range recordWriters {
		for _, role := range executors(t, fn) {
			if role == "record_read" || role == "record_verify" {
				t.Errorf("%s can execute %s", role, fn)
			}
		}
	}
	for _, role := range []string{"record_read", "record_verify"} {
		for _, tbl := range recordTables {
			for _, verb := range writeVerbs {
				if hasTablePriv(t, role, tbl, verb) {
					t.Errorf("%s holds %s on %s", role, verb, tbl)
				}
			}
		}
	}
}

// The verifier (record_verify) is the one role that reads the Record tables directly, across tenants, by design (REC-05); it never writes. This pins
// exactly what it holds so that a widening is a test failure, not a drift.
func TestTheVerifierReadsDirectlyAndHoldsNothingElse(t *testing.T) {
	want := map[string]bool{"record_event:SELECT": true, "record_chain_head:SELECT": true}
	for _, tbl := range recordTables {
		for _, verb := range append([]string{"SELECT"}, writeVerbs...) {
			if got := hasTablePriv(t, "record_verify", tbl, verb); got != want[tbl+":"+verb] {
				t.Errorf("record_verify %s on %s = %v, want %v", verb, tbl, got, want[tbl+":"+verb])
			}
		}
	}
}

// Who may call each Record function is pinned EXACTLY: the expected set must be the whole set, so a missing grant fails as surely as an extra one.
// Among the runtime roles only (migrations 0001 to 0014; the migration login reaches them through its membership of record_append, outside this list).
func TestExactlyTheExpectedRolesCallEachRecordFunction(t *testing.T) {
	powers := []string{"evidence_rw", "execution_rw", "orchestrator_rw", "outcome_rw", "record_append", "record_emitter", "trust_rw"}
	want := map[string][]string{
		"record_insert_t":        powers,
		"record_lock_head_t":     powers,
		"record_read_chain_t":    {"record_append", "record_read"},
		"record_login_tenant":    {"record_append"},
		"provision_tenant_login": {"record_append"},
	}
	for fn, roles := range want {
		got := executors(t, fn)
		if !slices.Equal(got, roles) { // both sorted by name
			t.Errorf("%s: executable by %v, want exactly %v", fn, got, roles)
		}
	}
}

// provision_emitter_login arrives with noema#181. Until then this test is skipped, visibly, instead of passing on an empty result; once the function
// exists it must be executable by record_append alone.
func TestExactlyRecordAppendProvisionsEmitterLogins(t *testing.T) {
	pool := migrated(t)
	var exists int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM pg_proc WHERE pronamespace = 'public'::regnamespace AND proname = 'provision_emitter_login'`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists == 0 {
		t.Skip("provision_emitter_login does not exist until noema#181 merges: not covered, not proven")
	}
	if got := executors(t, "provision_emitter_login"); !slices.Equal(got, []string{"record_append"}) {
		t.Errorf("provision_emitter_login: executable by %v, want exactly [record_append]", got)
	}
}

// There is no database role for operators ("ops", A1): an operator has no path to the Record at the database, whatever the edge allows.
func TestThereIsNoOperatorRoleInTheDatabase(t *testing.T) {
	pool := migrated(t)
	var n int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM pg_roles WHERE rolname ~ '^(ops|operator|audit)(_|$)'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d operator or audit roles exist; the A20 audit covers record_read and record_verify only", n)
	}
}

// SR disposition on noema#241, condition (2) of F1: no runtime, tenant, audit or emitter role is a member of record_verify, directly or through another role.
// The verifier's own login (noema_verify, created by deploy/compose/postgres/init.sh) is the only member there may be; in a test database there is none.
func TestNoRuntimeRoleIsAMemberOfTheVerifierRole(t *testing.T) {
	pool := migrated(t)
	for _, role := range runtimeRoles {
		if role == "record_verify" {
			continue
		}
		var member bool
		if err := pool.QueryRow(ctx(t), `SELECT pg_has_role($1, 'record_verify', 'MEMBER')`, role).Scan(&member); err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if member {
			t.Errorf("%s is a member of record_verify, directly or through another role: it would read every tenant's Record", role)
		}
	}
}
