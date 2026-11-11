package db_test

// Track 3 (rulings A6 and A13's sibling): every path that can read or write decision_record or decision_grant, found in the catalog and not by
// asking the author, must be on a reviewed list. A new function, view, rule, policy, publication or grant on either table fails this test until a
// person has read it for a row-security bypass (SECURITY DEFINER, owner access, search_path). The list is also the audit handed to the Security
// Reviewer (docs/security/decision-tables-row-security-audit.md): SR should test what is NOT on it, and try to add something to it.

import (
	"sort"
	"strings"
	"testing"
)

// reviewed names every catalog object that mentions one of the two tables, with the reason it cannot bypass tenant scoping.
var reviewedPaths = map[string]string{
	"function decision_grant_requires_allow": "trigger on decision_grant; SECURITY INVOKER, so it reads public.decision_record as the inserting login and row security applies to it (a foreign tenant's ALLOW is invisible); search_path = pg_catalog, pg_temp so a temporary table cannot stand in for it",
	"function decision_grant_guard":          "trigger on decision_grant (migration 0001); compares columns of OLD and NEW only; reads no other table",
	"function decision_record_tenant_guard":  "trigger on decision_record; SECURITY DEFINER with search_path = pg_catalog, pg_temp; reads public.tenant_login and pg_roles, never decision_record",
	"function outcome_record_verdict":        "Outcome (migration 0007); SECURITY DEFINER, reads decision_grant to check a verdict's correlation; its policy sees only the caller's tenant because the policy keys on session_user, and its search_path pins pg_temp last since migration 0012",
	"function reject_mutation":               "trigger function that always raises; reads no table",
}

func TestEveryPathThatTouchesTheDecisionTablesHasBeenReviewed(t *testing.T) {
	pool := migrated(t)
	found := map[string]bool{}
	// functions whose source names either table, and every trigger function attached to either table
	rows, err := pool.Query(ctx(t), `
		SELECT 'function ' || p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND (p.prosrc ~ 'decision_record|decision_grant')
		UNION
		SELECT 'function ' || p.proname FROM pg_trigger tg JOIN pg_proc p ON p.oid = tg.tgfoid
		 WHERE NOT tg.tgisinternal AND tg.tgrelid IN ('decision_record'::regclass, 'decision_grant'::regclass)
		UNION
		SELECT 'view ' || c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind IN ('v', 'm') AND pg_get_viewdef(c.oid) ~ 'decision_record|decision_grant'
		UNION
		SELECT 'rule ' || r.rulename FROM pg_rewrite r JOIN pg_class c ON c.oid = r.ev_class
		 WHERE c.relname IN ('decision_record', 'decision_grant') AND r.rulename <> '_RETURN'
		UNION
		SELECT 'publication ' || p.pubname FROM pg_publication p`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		found[s] = true
	}
	rows.Close()
	var unreviewed []string
	for s := range found {
		if _, ok := reviewedPaths[s]; !ok {
			unreviewed = append(unreviewed, s)
		}
	}
	sort.Strings(unreviewed)
	if len(unreviewed) > 0 {
		t.Fatalf("catalog objects that touch decision_record or decision_grant and have not been reviewed for a row-security bypass: %s", strings.Join(unreviewed, ", "))
	}
	for s := range reviewedPaths {
		if !found[s] {
			t.Errorf("the reviewed list names %q, which the catalog no longer has: remove it or find where it went", s)
		}
	}
}

// A SECURITY DEFINER function that reads either table must pin its search_path with pg_temp LAST. Without it PostgreSQL searches the session's
// temporary schema first for tables, so a login with the TEMPORARY privilege could create pg_temp.decision_grant and have the function read the
// caller's forged rows instead of the real, row-secured table. Functions of the decision tables themselves must also not read them as definers.
func TestEveryDefinerThatReadsTheDecisionTablesPinsItsSearchPathWithTempLast(t *testing.T) {
	pool := migrated(t)
	rows, err := pool.Query(ctx(t), `
		SELECT p.proname, coalesce(array_to_string(p.proconfig, ','), ''),
		       p.prosrc ~* '(from|join|update|into)\s+(public\.)?decision_(record|grant)\y'
		  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.prosecdef`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n, readers := 0, 0
	for rows.Next() {
		var name, cfg string
		var reads bool
		if err := rows.Scan(&name, &cfg, &reads); err != nil {
			t.Fatal(err)
		}
		n++
		if !reads {
			continue
		}
		readers++
		if !strings.HasPrefix(cfg, "search_path=") || !strings.HasSuffix(cfg, ", pg_temp") {
			t.Errorf("SECURITY DEFINER function %s reads a decision table and its search_path is %q: pg_temp must be last", name, cfg)
		}
		if strings.HasPrefix(name, "decision_") {
			t.Errorf("%s belongs to the decision tables and must not read them as a definer", name)
		}
	}
	if n == 0 || readers == 0 {
		t.Fatalf("found %d definer functions, %d of them readers of the decision tables: the query is not looking at the schema", n, readers)
	}
}

// The definer functions that do NOT read the decision tables and still leave pg_temp unpinned are not mine to change (the Record functions are Core
// Engineer 1's under ruling A6; the Outcome ones are the Outcome owner's). They are named here so that the list can only shrink: the day one is
// fixed this test fails until it is removed, and a new unpinned definer function fails it at once. They are in the audit handed to the Security Reviewer.
var definersWithoutTempPinned = map[string]string{
	"outcome_verdict_complete":       "Outcome (migration 0007); reads outcome tables only",
	"provision_tenant_login":         "Record (migration 0006); Core Engineer 1, ruling A6",
	"record_insert_t":                "Record (migration 0004); Core Engineer 1, ruling A6",
	"record_lock_head_t":             "Record (migration 0004); Core Engineer 1, ruling A6",
	"record_login_tenant":            "Record (migration 0004); Core Engineer 1, ruling A6",
	"record_read_chain_t":            "Record (migration 0004); Core Engineer 1, ruling A6",
	"recovery_request_verdict_guard": "Outcome/Recovery (migration 0007); reads recovery tables only",
}

func TestTheListOfDefinersWithoutPgTempPinnedOnlyShrinks(t *testing.T) {
	pool := migrated(t)
	rows, err := pool.Query(ctx(t), `
		SELECT p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.prosecdef AND NOT (coalesce(array_to_string(p.proconfig, ','), '') ~ 'search_path=.*, pg_temp$')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		seen[name] = true
		if _, ok := definersWithoutTempPinned[name]; !ok {
			t.Errorf("SECURITY DEFINER function %s does not pin pg_temp last and is not on the known list", name)
		}
	}
	for name := range definersWithoutTempPinned {
		if !seen[name] {
			t.Errorf("%s now pins pg_temp last: remove it from the known list", name)
		}
	}
}

// Who can reach the tables at all: the privileges, the owner and the forced policies, read from the catalog.
func TestOnlyTheReviewedRolesHoldAPrivilegeOnTheDecisionTables(t *testing.T) {
	pool := migrated(t)
	want := map[string]string{ // table.role -> privileges (the sorted letters)
		"decision_record.trust_rw": "ar", "decision_record.execution_rw": "r", "decision_record.outcome_rw": "r",
		"decision_grant.trust_rw": "ar", "decision_grant.execution_rw": "r", "decision_grant.outcome_rw": "r",
	}
	got := map[string]string{}
	rows, err := pool.Query(ctx(t), `
		SELECT c.relname || '.' || r.rolname,
		       (CASE WHEN has_table_privilege(r.oid, c.oid, 'INSERT') THEN 'a' ELSE '' END) ||
		       (CASE WHEN has_table_privilege(r.oid, c.oid, 'SELECT') THEN 'r' ELSE '' END) ||
		       (CASE WHEN has_table_privilege(r.oid, c.oid, 'UPDATE') THEN 'w' ELSE '' END) ||
		       (CASE WHEN has_table_privilege(r.oid, c.oid, 'DELETE') THEN 'd' ELSE '' END) ||
		       (CASE WHEN has_table_privilege(r.oid, c.oid, 'TRUNCATE') THEN 't' ELSE '' END)
		  FROM pg_class c, pg_roles r
		 WHERE c.relname IN ('decision_record', 'decision_grant') AND c.relnamespace = 'public'::regnamespace
		   AND NOT r.rolsuper AND r.rolname !~ '^(pg_|postgres$)' AND r.rolname !~ '__'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		if v != "" {
			got[k] = v
		}
	}
	rows.Close()
	for k, v := range got {
		if w, ok := want[k]; !ok {
			t.Errorf("%s holds privileges %q but is not on the reviewed list", k, v)
		} else if w != v && !(k == "decision_grant.execution_rw" && v == "rw") {
			t.Errorf("%s holds %q, reviewed %q", k, v, w)
		}
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("%s no longer holds any privilege: the reviewed list is out of date", k)
		}
	}
}
