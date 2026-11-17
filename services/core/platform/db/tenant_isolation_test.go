package db_test

// SEC-08 (REQ-SEC-004; INV-11, INV-12; ADV-12): the cross-tenant suite starts from the catalog, not from the author's memory. Every table in the schema that holds a tenant_id must
// force row-level security with a policy bound to the login's tenant, so a table added without one fails here. The behavioural refusals (a read, a write, a grant, a Record query
// across tenants) are in the packages that own each table and are named in status/SEC-08.md; this test makes sure no table escapes them by having no policy at all.
// Two things are reviewed by hand and listed: the registry tables, and the verification logins that read every tenant's chain and cannot write.

import (
	"regexp"
	"strings"
	"testing"
)

// registryTables hold a tenant_id as their key, not as tenant data. A power login may read `tenant` (SELECT only: the ledger reads its own tenant's kill switch) and nothing of
// tenant_login. FINDING for the Security Reviewer (status/SEC-08.md): `tenant` has no row-level security, so any power login can list every tenant's ID and name; the smallest
// fix is a policy on it bound to the login's tenant (migration 0029, C1's number), with the verification logins reading all.
var registryTables = map[string]string{
	"tenant":       "the registry of tenants: tenant_id is its key; power logins may SELECT it and nothing more (see the finding above)",
	"tenant_login": "the map from a login to its tenant and power; read only by the SECURITY DEFINER *_login_tenant functions",
}
var registryReadable = map[string]bool{"tenant": true}

// verificationRoles read every tenant's chain to verify its hash links (REC-05, SEC-09): SELECT only, by policy and by grant.
var verificationRoles = map[string]bool{"evidence_verify": true, "record_verify": true}

type policyRow struct {
	table, name, cmd, roles, using, check string
}

func TestEveryTenantTableForcesRowSecurityWithAPolicyBoundToTheLoginsTenant(t *testing.T) {
	pool := migrated(t)
	rows, err := pool.Query(ctx(t), `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity
		  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
		   AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)
		 ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	type table struct{ rls, force bool }
	tables := map[string]table{}
	for rows.Next() {
		var n string
		var tb table
		if err := rows.Scan(&n, &tb.rls, &tb.force); err != nil {
			t.Fatal(err)
		}
		tables[n] = tb
	}
	rows.Close()
	if len(tables) < 15 {
		t.Fatalf("only %d tables hold a tenant_id: the catalog query is not reading the schema", len(tables))
	}
	pol, err := pool.Query(ctx(t), `
		SELECT c.relname, p.polname, p.polcmd::text,
		       coalesce((SELECT string_agg(r.rolname, ',' ORDER BY r.rolname) FROM pg_roles r WHERE r.oid = ANY (p.polroles)), 'PUBLIC'),
		       coalesce(pg_get_expr(p.polqual, p.polrelid), ''), coalesce(pg_get_expr(p.polwithcheck, p.polrelid), '')
		  FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public'`)
	if err != nil {
		t.Fatal(err)
	}
	byTable := map[string][]policyRow{}
	for pol.Next() {
		var r policyRow
		if err := pol.Scan(&r.table, &r.name, &r.cmd, &r.roles, &r.using, &r.check); err != nil {
			t.Fatal(err)
		}
		byTable[r.table] = append(byTable[r.table], r)
	}
	pol.Close()

	for name, tb := range tables {
		if _, ok := registryTables[name]; ok {
			continue
		}
		if !tb.rls || !tb.force {
			t.Errorf("%s holds a tenant_id and row-level security is enabled=%t forced=%t (a table owner would bypass it)", name, tb.rls, tb.force)
		}
		if len(byTable[name]) == 0 {
			t.Errorf("%s has row-level security and no policy: nothing is readable, and the next policy added is unreviewed", name)
		}
		for _, p := range byTable[name] {
			// every present expression is exactly the bound one (the Security Reviewer's blocking finding on noema#658: "contains _login_tenant(" accepted a loosened policy)
			bound := (p.using != "" || p.check != "") && (p.using == "" || boundExpression(p.using)) && (p.check == "" || boundExpression(p.check))
			switch {
			case bound:
				// the policy compares tenant_id with the tenant of the session's login: the only ways in are that tenant's own rows
			case p.cmd == "r" && strings.TrimSpace(p.using) == "true" && p.check == "" && onlyVerificationRoles(p.roles):
				// a verification login reads the chain of every tenant and can write nothing (SELECT-only policy, SELECT-only grant)
			default:
				t.Errorf("%s policy %s (%s for %s) is not bound to the login's tenant: using %q, check %q", name, p.name, p.cmd, p.roles, p.using, p.check)
			}
		}
	}
	for name := range registryTables {
		if _, ok := tables[name]; !ok {
			t.Errorf("registryTables names %s, which no longer holds a tenant_id", name)
		}
	}
}

// boundExpression is true only for what pg_get_expr prints for a policy bound to the login's tenant: `(tenant_id = <name>_login_tenant(...))`, optionally narrowed by an AND of
// equalities of a text column with a literal (decision_grant_write: `(status = 'ISSUED'::text)`). An OR, a `true`, a comparison of tenant_id with anything else or any other
// shape is refused: it could make every row visible to every login.
var (
	boundShape  = `\(tenant_id = [a-z]+_login_tenant\((?:'[a-z]+'::text)?\)\)`
	boundOnly   = regexp.MustCompile(`^` + boundShape + `$`)
	boundNarrow = regexp.MustCompile(`^\(` + boundShape + `(?: AND \([a-z_]+ = '[A-Za-z_]+'::text\))+\)$`)
)

func boundExpression(expr string) bool {
	e := strings.TrimSpace(expr)
	return boundOnly.MatchString(e) || boundNarrow.MatchString(e)
}

func TestTheBoundPolicyMatcherRefusesALoosenedPolicy(t *testing.T) {
	accepted := []string{
		"(tenant_id = decision_login_tenant())",
		"(tenant_id = evidence_login_tenant())",
		"(tenant_id = record_login_tenant('write'::text))",
		"((tenant_id = decision_login_tenant()) AND (status = 'ISSUED'::text))",
	}
	refused := []string{
		"true",
		"(tenant_id = tenant_id)",
		"((tenant_id = tenant_id) OR (evidence_login_tenant() IS NOT NULL))",
		"(true OR (tenant_id = decision_login_tenant()))",
		"((tenant_id = decision_login_tenant()) OR true)",
		"((tenant_id = decision_login_tenant()) OR (status = 'ISSUED'::text))",
		"(tenant_id <> decision_login_tenant())",
		"(tenant_id = 'ten_aa'::text)",
		"(decision_login_tenant() IS NOT NULL)",
		"(tenant_id = decision_login_tenant()) OR true",
		"((tenant_id = decision_login_tenant()) AND true)",
		"",
	}
	for _, e := range accepted {
		if !boundExpression(e) {
			t.Errorf("a bound policy was refused: %s", e)
		}
	}
	for _, e := range refused {
		if boundExpression(e) {
			t.Errorf("a loosened policy was accepted as bound: %q", e)
		}
	}
}

// A table of the schema with no tenant_id is outside the catalog query above, so it is named here with the reason it holds no tenant data; a new one fails until reviewed.
var tablesWithNoTenantID = map[string]string{
	"schema_migration": "the migration ledger: file names and checksums, no tenant data",
	"signing_key":      "the public keys of the core's own emitter identities (migration 0013): key material, no tenant data",
}

func TestEveryTableWithNoTenantIDHasBeenReviewed(t *testing.T) {
	pool := migrated(t)
	rows, err := pool.Query(ctx(t), `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
		   AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped) ORDER BY 1`)
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
		if _, ok := tablesWithNoTenantID[name]; !ok {
			t.Errorf("%s has no tenant_id and is not on the reviewed list: if it holds tenant data it needs one and a policy", name)
		}
	}
	for name := range tablesWithNoTenantID {
		if !seen[name] {
			t.Errorf("tablesWithNoTenantID names %s, which no longer exists without a tenant_id", name)
		}
	}
}

func onlyVerificationRoles(roles string) bool {
	for _, r := range strings.Split(roles, ",") {
		if !verificationRoles[r] {
			return false
		}
	}
	return roles != ""
}

// A verification login can read and nothing else, on every table it can see (grants, not only policies).
func TestTheVerificationLoginsCanOnlyReadAndThePowerLoginsCanOnlyReadTheRegistry(t *testing.T) {
	pool := migrated(t)
	for role := range verificationRoles {
		rows, err := pool.Query(ctx(t), `
			SELECT table_name, privilege_type FROM information_schema.role_table_grants
			 WHERE grantee = $1 AND table_schema = 'public' AND privilege_type <> 'SELECT'`, role)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var tbl, priv string
			if err := rows.Scan(&tbl, &priv); err != nil {
				t.Fatal(err)
			}
			t.Errorf("%s holds %s on %s: a verification login may only read", role, priv, tbl)
		}
		rows.Close()
	}
	for _, role := range []string{"trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "orchestrator_rw"} {
		for table := range registryTables {
			for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
				var can bool
				if err := pool.QueryRow(ctx(t), `SELECT has_table_privilege($1, $2, $3)`, role, "public."+table, priv).Scan(&can); err != nil {
					t.Fatal(err)
				}
				if can && !(priv == "SELECT" && registryReadable[table]) {
					t.Errorf("%s holds %s on %s (%s)", role, priv, table, registryTables[table])
				}
			}
		}
	}
}

// A session with no tenant mapping gets no tenant from any of the login-tenant functions, so every policy compares tenant_id with NULL and matches nothing: fail closed.
func TestASessionWithNoTenantMappingGetsNoTenantFromAnyLoginTenantFunction(t *testing.T) {
	pool := migrated(t)
	rows, err := pool.Query(ctx(t), `
		SELECT p.proname, pg_get_function_identity_arguments(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.proname LIKE '%\_login\_tenant' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	type fn struct{ name, args string }
	var fns []fn
	for rows.Next() {
		var f fn
		if err := rows.Scan(&f.name, &f.args); err != nil {
			t.Fatal(err)
		}
		fns = append(fns, f)
	}
	rows.Close()
	if len(fns) < 3 {
		t.Fatalf("found %d login-tenant functions, want at least decision, evidence and policy", len(fns))
	}
	for _, f := range fns {
		call := f.name + "()"
		if f.args != "" {
			call = f.name + "('write')" // record_login_tenant(text): the power the login writes as
		}
		var tenant *string
		if err := pool.QueryRow(ctx(t), `SELECT `+call).Scan(&tenant); err != nil {
			if strings.Contains(err.Error(), "not mapped to a tenant") {
				continue // record_login_tenant refuses an unmapped login outright, which is the same refusal, earlier
			}
			t.Errorf("%s: %v", call, err)
			continue
		}
		if tenant != nil {
			t.Errorf("%s returned %q for the administrator's session, which has no tenant mapping", call, *tenant)
		}
	}
}
