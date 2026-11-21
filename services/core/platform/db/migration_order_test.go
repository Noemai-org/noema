package db_test

// Ruling A113 (the Architect, 10 Nov 2026, noema#393): the migration numbers stay as assigned (the gap 0011 to 0016 is accepted) and the files merge
// in strict ascending order. Out-of-order merging is not allowed. But 0017 (#265) is already on main, ahead of the lower numbers, so every database that holds
// 0017 is later given 0011 to 0016 one after the other, then 0018 and the rest. That is safe only if such a database ends with the same catalog as a database
// migrated in name order: the same privileges, triggers, policies, constraints, indexes AND function bodies. Migrate applies every unapplied file in name
// order and does not look at the highest applied number, so this is a property the files must keep. The test migrates a database that holds 0001 to 0010 and
// 0017 and then every other file, and compares. A later file that CREATE OR REPLACEs a function that 0017 also touches would make that database lose a body: this
// test fails then. It describes the upgrade of a database that holds 0017; it does not make a merge path out of order (the order is the ledger's, noema#183).
// REQ-DATA-001, REQ-AUD-006; AGENTS.md section 7 (forward-only migrations).

import (
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

var migrationNumber = regexp.MustCompile(`^([0-9]{4})_`)

// catalog is a sorted, text snapshot of everything a migration can change in schema public (and noema_sig when it exists).
func catalog(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	queries := map[string]string{
		"function": `SELECT n.nspname || '.' || p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ') owner=' || pg_get_userbyid(p.proowner)
				|| ' acl=' || coalesce(p.proacl::text, '') || ' config=' || coalesce(p.proconfig::text, '') || ' def=' || pg_get_functiondef(p.oid)
			FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname IN ('public', 'noema_sig') AND p.prokind = 'f'
			  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.deptype = 'e')`,
		"trigger": `SELECT c.relname || ' ' || t.tgname || ' enabled=' || t.tgenabled::text || ' ' || pg_get_triggerdef(t.oid)
			FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE NOT t.tgisinternal AND n.nspname IN ('public', 'noema_sig')`,
		"relation": `SELECT n.nspname || '.' || c.relname || ' kind=' || c.relkind::text || ' owner=' || pg_get_userbyid(c.relowner) || ' acl=' || coalesce(c.relacl::text, '')
				|| ' rls=' || c.relrowsecurity || ' force=' || c.relforcerowsecurity
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname IN ('public', 'noema_sig') AND c.relkind IN ('r', 'v', 'S', 'p')`,
		"column": `SELECT c.relname || '.' || a.attname || ' ' || format_type(a.atttypid, a.atttypmod) || ' notnull=' || a.attnotnull
				|| ' default=' || coalesce(pg_get_expr(d.adbin, d.adrelid), '') || ' acl=' || coalesce(a.attacl::text, '')
			FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
			WHERE n.nspname IN ('public', 'noema_sig') AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped`,
		"constraint": `SELECT c.relname || ' ' || k.conname || ' ' || pg_get_constraintdef(k.oid)
			FROM pg_constraint k JOIN pg_class c ON c.oid = k.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname IN ('public', 'noema_sig')`,
		"index": `SELECT schemaname || '.' || tablename || ' ' || indexdef FROM pg_indexes WHERE schemaname IN ('public', 'noema_sig')`,
		"policy": `SELECT schemaname || '.' || tablename || ' ' || policyname || ' ' || permissive || ' ' || roles::text || ' ' || cmd || ' '
				|| coalesce(qual, '') || ' ' || coalesce(with_check, '')
			FROM pg_policies WHERE schemaname IN ('public', 'noema_sig')`,
	}
	var out []string
	for kind, q := range queries {
		rows, err := pool.Query(ctx(t), q)
		if err != nil {
			t.Fatalf("catalog %s: %v", kind, err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				t.Fatalf("catalog %s: %v", kind, err)
			}
			out = append(out, kind+": "+line)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("catalog %s: %v", kind, err)
		}
	}
	sort.Strings(out)
	return out
}

func diff(a, b []string) []string {
	in := func(xs []string) map[string]bool {
		m := map[string]bool{}
		for _, x := range xs {
			m[x] = true
		}
		return m
	}
	ma, mb := in(a), in(b)
	var out []string
	for _, x := range a {
		if !mb[x] {
			out = append(out, "only after the upgrade path: "+first(x))
		}
	}
	for _, x := range b {
		if !ma[x] {
			out = append(out, "only in the fresh database: "+first(x))
		}
	}
	return out
}

func first(s string) string {
	if len(s) > 400 {
		return s[:400] + "..."
	}
	return s
}

// shippedBeforeTheTrain is the migrations a database already held when 0011 to 0016 were still to come: 0001 to 0010, and 0017 (merged ahead of them, A113).
func shippedBeforeTheTrain(t *testing.T) fs.FS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		m := migrationNumber.FindStringSubmatch(e.Name())
		if m == nil || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if n, _ := strconv.Atoi(m[1]); n <= 10 || n == 17 {
			b, err := fs.ReadFile(migrations.FS, e.Name())
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = &fstest.MapFile{Data: b}
		}
	}
	return out
}

// A113: 0017 reached main before 0011 to 0016, which merge later in ascending order. A database that already holds 0017 and is then migrated with every other file in
// name order must end with the catalog of a fresh database. (Earlier versions of this test took each lower file out in turn, which also asks for an order that can no
// longer happen once 0011 and 0012 are merged: a database that holds 0016 and not 0012.)
func TestADatabaseThatHoldsOnly0017AheadOfTheTrainEndsWithTheCatalogOfAFreshOne(t *testing.T) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	var later []string
	for _, e := range entries {
		if m := migrationNumber.FindStringSubmatch(e.Name()); m != nil {
			if n, _ := strconv.Atoi(m[1]); n >= 11 && n != 17 {
				later = append(later, e.Name())
			}
		}
	}
	sort.Strings(later)
	if len(later) == 0 {
		t.Skip("no migration numbered 0011 or above (other than 0017) on this branch")
	}
	fresh, _ := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), fresh, migrations.FS); err != nil {
		t.Fatal(err)
	}
	want := catalog(t, fresh)
	if len(want) < 50 {
		t.Fatalf("the catalog snapshot has %d lines: the queries are looking in the wrong place", len(want))
	}
	upgraded, _ := testpg.NewDatabase(t)
	first, err := db.Migrate(ctx(t), upgraded, shippedBeforeTheTrain(t))
	if err != nil || len(first) == 0 {
		t.Fatalf("the database before the train: applied %v, %v", first, err)
	}
	second, err := db.Migrate(ctx(t), upgraded, migrations.FS)
	if err != nil || strings.Join(second, ",") != strings.Join(later, ",") {
		t.Fatalf("the second pass applied %v (%v), want %v in name order", second, err, later)
	}
	if d := diff(catalog(t, upgraded), want); len(d) > 0 {
		t.Fatalf("a database that held 0017 ahead of %s differs from a fresh one (%d lines):\n%s", strings.Join(later, ", "), len(d), strings.Join(d, "\n"))
	}
}
